local config = require("scripts.config")
local chunks = require("scripts.chunks")
local blueprint_position = require("scripts.blueprint-position")
local platform_hub = require("scripts.platform-hub")
local train_rails = require("scripts.train-rails")
local requests = require("scripts.requests")
local build_cost = require("scripts.build-cost")
local entity_types = require("scripts.rate-calculator.entity-types")

local blueprint = {}
local SPACE_PLATFORM_FOUNDATION = "space-platform-foundation"
local SPACE_PLATFORM_HUB = "space-platform-hub"
local signal_prototype_types = {
  item = "item",
  fluid = "fluid",
  virtual = "virtual_signal",
  ["space-location"] = "space_location",
  quality = "quality",
  entity = "entity",
  recipe = "recipe",
}

local function grow(box, other)
  if not box then
    return {
      left_top = { x = other.left_top.x, y = other.left_top.y },
      right_bottom = { x = other.right_bottom.x, y = other.right_bottom.y },
    }
  end

  box.left_top.x = math.min(box.left_top.x, other.left_top.x)
  box.left_top.y = math.min(box.left_top.y, other.left_top.y)
  box.right_bottom.x = math.max(box.right_bottom.x, other.right_bottom.x)
  box.right_bottom.y = math.max(box.right_bottom.y, other.right_bottom.y)
  return box
end

local function estimate_area(stack)
  local box
  local function include(position)
    box = grow(box, {
      left_top = { x = position.x + config.build_position.x - config.margin_tiles,
        y = position.y + config.build_position.y - config.margin_tiles },
      right_bottom = { x = position.x + config.build_position.x + config.margin_tiles,
        y = position.y + config.build_position.y + config.margin_tiles },
    })
  end

  local entities = stack.get_blueprint_entities()
  if entities then
    for _, entity in pairs(entities) do
      include(entity.position)
    end
  end

  local tiles = stack.get_blueprint_tiles()
  if tiles then
    for _, tile in pairs(tiles) do
      include(tile.position)
    end
  end

  return box or { left_top = config.build_position, right_bottom = config.build_position }
end

-- Tile ghosts have no meaningful collision box, so their footprint is derived
-- from the tile they occupy instead.
local function ghost_box(ghost)
  if ghost.type == "tile-ghost" then
    local position = ghost.position
    return {
      left_top = { x = position.x - 0.5, y = position.y - 0.5 },
      right_bottom = { x = position.x + 0.5, y = position.y + 0.5 },
    }
  end
  return ghost.selection_box
end

-- The ground an entity covers: its collision box (elevated rails select
-- above the ground they stand on); tiles cover their own tile.
local function footprint_box(ghost)
  if ghost.type == "tile-ghost" then
    return ghost_box(ghost)
  end
  return ghost.bounding_box or ghost.selection_box
end

-- Tile-aligned width and height of a box: an edge within 0.01 of a tile line
-- counts as on it, so collision boxes (inset from their tiles) round out.
local function tile_extent(box)
  local epsilon = 0.01
  return {
    width = math.ceil(box.right_bottom.x - epsilon) - math.floor(box.left_top.x + epsilon),
    height = math.ceil(box.right_bottom.y - epsilon) - math.floor(box.left_top.y + epsilon),
  }
end

local function blueprint_tile_box(tile)
  local position = tile.position
  return {
    left_top = { x = position.x, y = position.y },
    right_bottom = { x = position.x + 1, y = position.y + 1 },
  }
end

--- One materialization issue: an entity the analysis could not account for,
--- recorded with why (`cause`, see analysis.lua) and the metrics it leaves
--- unresolved.
local function issue_record(impacts, cause, entity_name)
  return {
    cause = cause,
    entity = entity_name,
    affects_materials = impacts.materials,
    affects_electricity = impacts.electricity,
    affects_pollution = impacts.pollution,
  }
end

--- Whether a prototype can only stand on terrain the blank analysis surface has
--- not got (an offshore pump needs water). Its ghost never revives, which is not
--- a failure of the analysis: what it would pump is unknown anyway.
local function needs_terrain(prototype)
  return prototype ~= nil and prototype.type == "offshore-pump"
end

local function placed_blueprint_tiles(tiles, origin, predicate)
  local placed_tiles = {}

  for _, tile in pairs(tiles or {}) do
    if predicate(tile) then
      table.insert(placed_tiles, {
        name = tile.name,
        position = {
          x = tile.position.x + origin.x,
          y = tile.position.y + origin.y,
        },
      })
    end
  end

  return placed_tiles
end

-- A blueprint with a platform hub snaps to a 256-tile grid around its hub, and
-- build_blueprint puts the grid cell nearest the blueprint's centre under the
-- build position: a platform longer than about 512 tiles was built whole cells
-- away from its foundation tiles and hub, which are placed at blueprint
-- coordinates. Absolute snapping with the grid at the origin builds the
-- blueprint at exactly its own coordinates while the build position lies in
-- the origin cell (config.build_position is the origin).
local HUB_GRID_TILES = 256

local function anchor_at_blueprint_coordinates(stack)
  stack.blueprint_snap_to_grid = { x = HUB_GRID_TILES, y = HUB_GRID_TILES }
  stack.blueprint_absolute_snapping = true
  stack.blueprint_position_relative_to_grid = { x = 0, y = 0 }
end

local function is_space_platform_foundation(tile)
  return tile.name == SPACE_PLATFORM_FOUNDATION
end

local function destroy_preplaced_tile_ghost(ghost)
  return ghost.type == "tile-ghost" and ghost.ghost_name == SPACE_PLATFORM_FOUNDATION
end

local function has_prototype(prototype_type, name)
  return type(name) == "string"
    and prototypes[prototype_type]
    and prototypes[prototype_type][name] ~= nil
end

-- Blueprint parameter placeholders (parameter-0..parameter-9) are not
-- prototypes but are valid in filters and conditions.
local function is_parameter_name(name)
  return type(name) == "string" and string.match(name, "^parameter%-%d+$") ~= nil
end

local function valid_named_prototype(name, prototype_type)
  if type(name) ~= "string" or is_parameter_name(name) then
    return true
  end
  local lookup_type = signal_prototype_types[prototype_type]
  if lookup_type then
    return has_prototype(lookup_type, name)
  end
  return has_prototype("item", name)
    or has_prototype("fluid", name)
    or has_prototype("virtual_signal", name)
    or has_prototype("space_location", name)
end

local function valid_signal(signal)
  if type(signal) ~= "table" or type(signal.name) ~= "string" then
    return true
  end
  return valid_named_prototype(signal.name, signal.type)
end

local function has_invalid_condition_signal(condition)
  return type(condition) == "table"
    and ((condition.first_signal ~= nil and not valid_signal(condition.first_signal))
      or (condition.second_signal ~= nil and not valid_signal(condition.second_signal)))
end

local function remove_invalid_condition_signals(control_behavior, counts)
  if type(control_behavior) ~= "table" then
    return
  end

  local condition = control_behavior.circuit_condition
  if has_invalid_condition_signal(condition) then
    control_behavior.circuit_condition = nil
    counts.signals = counts.signals + 1
  end

  local arithmetic = control_behavior.arithmetic_conditions
  if type(arithmetic) == "table"
      and ((arithmetic.first_signal ~= nil and not valid_signal(arithmetic.first_signal))
        or (arithmetic.second_signal ~= nil and not valid_signal(arithmetic.second_signal))
        or (arithmetic.output_signal ~= nil and not valid_signal(arithmetic.output_signal))) then
    control_behavior.arithmetic_conditions = nil
    counts.signals = counts.signals + 1
  end

  local decider = control_behavior.decider_conditions
  if type(decider) ~= "table" then
    return
  end
  for _, key in ipairs({ "conditions", "outputs", "else_outputs" }) do
    local values = decider[key]
    if type(values) == "table" then
      for index = #values, 1, -1 do
        local value = values[index]
        local invalid = has_invalid_condition_signal(value)
          or (type(value) == "table" and value.signal ~= nil and not valid_signal(value.signal))
        if invalid then
          table.remove(values, index)
          counts.signals = counts.signals + 1
        end
      end
      if #values == 0 then
        decider[key] = nil
      end
    end
  end
end

local function remove_invalid_schedule_conditions(schedules, counts)
  if type(schedules) ~= "table" then
    return
  end
  for _, schedule_entry in ipairs(schedules) do
    local schedule = type(schedule_entry) == "table" and schedule_entry.schedule
    local records = type(schedule) == "table" and schedule.records
    if type(records) == "table" then
      for _, record in ipairs(records) do
        local wait_conditions = type(record) == "table" and record.wait_conditions
        if type(wait_conditions) == "table" then
          for index = #wait_conditions, 1, -1 do
            local wait_condition = wait_conditions[index]
            local condition = type(wait_condition) == "table" and wait_condition.condition
            if has_invalid_condition_signal(condition) then
              table.remove(wait_conditions, index)
              counts.signals = counts.signals + 1
            end
          end
          if #wait_conditions == 0 then
            record.wait_conditions = nil
          end
        end
      end
    end
  end
end

local function sanitize_references(value, counts)
  if type(value) ~= "table" then
    return
  end

  for key, child in pairs(value) do
    if key == "filters" and type(child) == "table" then
      for index = #child, 1, -1 do
        local filter = child[index]
        if type(filter) == "table"
            and type(filter.name) == "string"
            and not valid_named_prototype(filter.name, filter.type) then
          table.remove(child, index)
          counts.filters = counts.filters + 1
        end
      end
      if #child == 0 then
        value[key] = nil
      end
    elseif key == "icons" and type(child) == "table" then
      -- An icon without its signal makes the whole blueprint unimportable.
      for index = #child, 1, -1 do
        local icon = child[index]
        if type(icon) == "table" and icon.signal ~= nil and not valid_signal(icon.signal) then
          table.remove(child, index)
          counts.signals = counts.signals + 1
        end
      end
      if #child == 0 then
        value[key] = nil
      end
    elseif key == "items" and type(child) == "table" then
      for index = #child, 1, -1 do
        local item = child[index]
        local name = type(item) == "table" and type(item.id) == "table" and item.id.name
        if type(name) == "string" and not has_prototype("item", name) then
          table.remove(child, index)
          counts.items = counts.items + 1
        end
      end
      if #child == 0 then
        value[key] = nil
      end
    elseif key == "signal" or key == "first_signal" or key == "second_signal"
        or key == "output_signal" or key == "temperature_signal" then
      if type(child) == "table" and not valid_signal(child) then
        value[key] = nil
        counts.signals = counts.signals + 1
      elseif type(child) == "table" then
        sanitize_references(child, counts)
      end
    elseif (key == "filter" or key == "fluid_filter") and type(child) == "string" then
      if not valid_named_prototype(child, key == "fluid_filter" and "fluid" or nil) then
        value[key] = nil
        counts.filters = counts.filters + 1
      end
    elseif type(child) == "table" then
      sanitize_references(child, counts)
    end
  end
end

local function sanitized_blueprint_string(blueprint_string)
  if type(blueprint_string) ~= "string"
      or string.sub(blueprint_string, 1, 1) ~= "0" then
    return nil, "blueprint string has an unsupported format"
  end

  local decoded, json = pcall(helpers.decode_string, string.sub(blueprint_string, 2))
  if not decoded or type(json) ~= "string" then
    return nil, "blueprint string could not be decoded"
  end

  local parsed, payload = pcall(helpers.json_to_table, json)
  if not parsed or type(payload) ~= "table" or type(payload.blueprint) ~= "table" then
    return nil, "blueprint string does not contain blueprint data"
  end

  local blueprint_data = payload.blueprint
  -- Prototype names swapped for a display panel; they are reported as issues
  -- because the panel stands in for an entity the analysis never saw.
  local replaced = {}
  local counts = { entities = 0, recipes = 0, filters = 0, signals = 0, items = 0 }
  if blueprint_data.entities ~= nil then
    if type(blueprint_data.entities) ~= "table" then
      return nil, "blueprint entities are invalid"
    end
    local entities = {}
    for _, entity in ipairs(blueprint_data.entities) do
      if type(entity) == "table"
          and type(entity.name) == "string"
          and not prototypes.entity[entity.name] then
        counts.entities = counts.entities + 1
        table.insert(replaced, entity.name)
        table.insert(entities, {
          entity_number = entity.entity_number,
          name = "display-panel",
          icon = {
            name= "signal-alert",
            type= "virtual"
          },
          position = entity.position,
          text = entity.name,
          always_show = true
        })
        log("Removed entity "..entity.name.. " from the blueprint")
      else
        if type(entity) == "table"
            and type(entity.recipe) == "string"
            and not prototypes.recipe[entity.recipe] then
          log("Removed recipe "..entity.recipe.. " from the blueprint")
          entity.recipe = nil
          entity.recipe_quality = nil
          counts.recipes = counts.recipes + 1
        end
        if type(entity) == "table" then
          remove_invalid_condition_signals(entity.control_behavior, counts)
        end
        table.insert(entities, entity)
      end
    end
    blueprint_data.entities = #entities > 0 and entities or nil
  end

  if blueprint_data.tiles ~= nil then
    if type(blueprint_data.tiles) ~= "table" then
      return nil, "blueprint tiles are invalid"
    end
    for _, tile in ipairs(blueprint_data.tiles) do
      if type(tile) == "table"
          and type(tile.name) == "string"
          and not prototypes.tile[tile.name] then
        tile.name = "hazard-concrete-left"
      end
    end
  end

  -- Grid snapping moves the build position (absolute snapping can shift it by
  -- half a grid cell), off the chunks generated for it. The preview is built at
  -- a fixed position, so the blueprint is rendered without its grid.
  blueprint_data["snap-to-grid"] = nil
  blueprint_data["absolute-snapping"] = nil
  blueprint_data["position-relative-to-grid"] = nil

  remove_invalid_schedule_conditions(blueprint_data.schedules, counts)
  sanitize_references(blueprint_data, counts)
  -- Rolling stock alone has no rails to stand on: lay it out on new ones.
  if train_rails.only_rolling_stock(blueprint_data.entities) then
    train_rails.lay(blueprint_data)
  end
  -- A platform hub is always created at the origin, so a platform keeps its
  -- original coordinates instead of being re-centred (and is built at them
  -- with absolute snapping, see anchor_at_blueprint_coordinates).
  local has_hub = false
  for _, entity in ipairs(blueprint_data.entities or {}) do
    if entity.name == SPACE_PLATFORM_HUB then has_hub = true end
  end
  if not has_hub then
    blueprint_position.center(blueprint_data)
  end

  local encoded, sanitized_json = pcall(helpers.table_to_json, payload)
  if not encoded or type(sanitized_json) ~= "string" then
    return nil, "sanitized blueprint could not be encoded"
  end
  local compressed, compressed_data = pcall(helpers.encode_string, sanitized_json)
  if not compressed or type(compressed_data) ~= "string" then
    return nil, "sanitized blueprint could not be compressed"
  end

  if counts.entities > 0 then
    log("Factorio Expo removed " .. tostring(counts.entities) ..
      " entities with unavailable prototypes before import")
  end
  if counts.recipes > 0 then
    log("Factorio Expo cleared " .. tostring(counts.recipes) ..
      " recipes with unavailable prototypes before import")
  end
  if counts.filters > 0 or counts.signals > 0 or counts.items > 0 then
    log("Factorio Expo removed " .. tostring(counts.filters) .. " filters, " ..
      tostring(counts.signals) .. " circuit signals, and " ..
      tostring(counts.items) .. " item requests with unavailable prototypes before import")
  end
  return "0" .. compressed_data, nil, replaced
end

local EMPTY_BLUEPRINT = "blueprint is empty"

local function imported_stack(blueprint_string)
  local clean_string, sanitize_error, replaced = sanitized_blueprint_string(blueprint_string)
  if not clean_string then
    return nil, nil, sanitize_error
  end

  local inventory = game.create_inventory(1)
  local stack = inventory[1]
  stack.set_stack({ name = "blueprint", count = 1 })

  -- Factorio binds API methods to their object, so import_stack takes only data.
  local imported, import_result = pcall(stack.import_stack, clean_string)
  -- -1 means the blueprint imported with errors; render the supported content.
  if not imported or (import_result ~= 0 and import_result ~= -1) then
    inventory.destroy()
    return nil, nil, "blueprint string could not be imported"
  end

  if not stack.is_blueprint_setup() then
    inventory.destroy()
    return nil, nil, EMPTY_BLUEPRINT
  end

  return stack, inventory, nil, replaced
end

function blueprint.needs_space_platform(blueprint_string)
  local stack, inventory, import_error = imported_stack(blueprint_string)
  if not stack then
    if import_error == EMPTY_BLUEPRINT then
      return false
    end
    return false, import_error
  end

  local entities = stack.get_blueprint_entities()
  if entities then
    for _, entity in pairs(entities) do
      if entity.name == SPACE_PLATFORM_HUB then
        inventory.destroy()
        return true
      end
    end
  end

  inventory.destroy()
  return false
end

local ROLLING_STOCK = {
  ["locomotive"] = true,
  ["cargo-wagon"] = true,
  ["fluid-wagon"] = true,
  ["artillery-wagon"] = true,
}

-- Ghosts are revived in tiers: elevated rails need their ramps and supports
-- first, and rolling stock needs the rails it stands on.
local RAIL_FOUNDATIONS = {
  ["rail-ramp"] = true,
  ["rail-support"] = true,
}

local ELEVATED_RAILS = {
  ["elevated-straight-rail"] = true,
  ["elevated-curved-rail-a"] = true,
  ["elevated-curved-rail-b"] = true,
  ["elevated-half-diagonal-rail"] = true,
}

local function revival_tier(ghost)
  if not (ghost.valid and ghost.type == "entity-ghost") then
    return 3
  end
  local kind = ghost.ghost_type
  if RAIL_FOUNDATIONS[kind] then
    return 1
  elseif ELEVATED_RAILS[kind] then
    return 2
  elseif ROLLING_STOCK[kind] then
    return 4
  end
  return 3
end

--- Orders ghosts by revival tier, keeping the build order inside a tier.
local function revival_order(ghosts)
  local tiers = { {}, {}, {}, {} }
  for _, ghost in ipairs(ghosts) do
    local tier = tiers[revival_tier(ghost)]
    tier[#tier + 1] = ghost
  end
  local ordered = {}
  for _, tier in ipairs(tiers) do
    for _, ghost in ipairs(tier) do
      ordered[#ordered + 1] = ghost
    end
  end
  return ordered
end

local function revive_module_requests(proxy, ghost_name, issues)
  if not requests.fulfill_modules(proxy) then
    -- Missing modules change the machine's speed, so materials are unresolved.
    local issue = issue_record(entity_types.metric_impacts(prototypes.entity[ghost_name]), "missing_modules", ghost_name)
    issue.affects_materials = true
    table.insert(issues, issue)
  end
end

-- What an empty blueprint renders as: a small patch of bare ground, so the
-- preview is an empty image and the analysis sees no entities.
local EMPTY_HALF_SIZE = 3

local function empty_placement(surface)
  local position = config.build_position
  local box = {
    left_top = { x = position.x - EMPTY_HALF_SIZE, y = position.y - EMPTY_HALF_SIZE },
    right_bottom = { x = position.x + EMPTY_HALF_SIZE, y = position.y + EMPTY_HALF_SIZE },
  }
  local generated, generation_error = pcall(chunks.generate, surface, box, config.chunk_padding)
  if not generated then
    return nil, tostring(generation_error)
  end
  return {
    box = box,
    footprint = { width = 1, height = 1 },
    entities = {},
    issues = {},
    build_items = build_cost.items({}, nil),
  }
end

--- Imports a blueprint string and materialises it on the given surface.
--- Returns the placement (`box` the bounding box to frame, `footprint` its
--- tile-aligned {width, height}, `entities` the revived live entities,
--- `issues` the materialization issues, `build_items` what it costs to
--- build), or nil and an error.
function blueprint.render(surface, force, blueprint_string)
  local stack, inventory, import_error, replaced = imported_stack(blueprint_string)
  if not stack then
    if import_error == EMPTY_BLUEPRINT then
      return empty_placement(surface)
    end
    return nil, import_error
  end

  local position = config.build_position
  local blueprint_entities = stack.get_blueprint_entities() or {}
  local build_items = build_cost.items(blueprint_entities, stack.get_blueprint_tiles())
  local generated, generation_error = pcall(chunks.generate, surface, estimate_area(stack), config.chunk_padding)
  if not generated then
    inventory.destroy()
    return nil, tostring(generation_error)
  end

  local foundation_tiles = placed_blueprint_tiles(stack.get_blueprint_tiles(), position, is_space_platform_foundation)
  if #foundation_tiles > 0 then
    surface.set_tiles(foundation_tiles, true, true, true, false)
  end
  local hub_ok, hub = pcall(platform_hub.place, surface, force, blueprint_entities, position)
  if not hub_ok then
    inventory.destroy()
    return nil, tostring(hub)
  end
  if hub then
    anchor_at_blueprint_coordinates(stack)
  end

  -- superforced so nothing is skipped because of leftover obstructions.
  local built, ghosts = pcall(stack.build_blueprint, {
    surface = surface,
    force = force,
    position = position,
    build_mode = defines.build_mode.superforced,
    skip_fog_of_war = false,
  })
  inventory.destroy()

  if not built then
    local build_error = "blueprint placement failed: " .. tostring(ghosts)
    log("Factorio Expo " .. build_error)
    return nil, build_error
  end

  if #ghosts == 0 and #foundation_tiles == 0 and not hub then
    log("Factorio Expo blueprint produced no placeable entities or tiles (imported_entities=" ..
      tostring(#blueprint_entities) .. ", surface=" .. surface.name .. ", force=" .. force.name .. ")")
    return empty_placement(surface)
  end

  local box
  local ground
  local live_entities = {}
  local issues = {}
  local materialized_records = {}
  for _, replaced_name in ipairs(replaced) do
    table.insert(issues, issue_record(entity_types.metric_impacts(nil), "unavailable_prototype", replaced_name))
  end
  if hub then
    if not hub.valid then
      return nil, "space platform hub was removed during blueprint placement"
    end
    box = grow(box, hub.selection_box)
    ground = grow(ground, hub.bounding_box or hub.selection_box)
    live_entities[#live_entities + 1] = hub
    materialized_records[SPACE_PLATFORM_HUB] = 1
  end
  for _, tile in pairs(foundation_tiles) do
    local tile_box = blueprint_tile_box(tile)
    box = grow(box, tile_box)
    ground = grow(ground, tile_box)
  end

  for _, ghost in ipairs(ghosts) do
    if ghost.valid then
      box = grow(box, ghost_box(ghost))
      ground = grow(ground, footprint_box(ghost))
      if ghost.type == "entity-ghost" then
        materialized_records[ghost.ghost_name] = (materialized_records[ghost.ghost_name] or 0) + 1
      end
    end
  end

  for _, record in ipairs(blueprint_entities) do
    if materialized_records[record.name] and materialized_records[record.name] > 0 then
      materialized_records[record.name] = materialized_records[record.name] - 1
    else
      local prototype = prototypes.entity[record.name]
      if not needs_terrain(prototype) and (not prototype or entity_types.is_supported(prototype)) then
        table.insert(issues, issue_record(entity_types.metric_impacts(prototype), "entity_not_materialized", record.name))
      end
    end
  end

  -- Ghosts render as translucent placeholders, so turn them into real entities
  -- before the screenshot is taken, in `revival_order` tiers.
  local failed = {}
  for _, ghost in ipairs(revival_order(ghosts)) do
    if ghost.valid then
      if hub and ghost.type == "entity-ghost" and ghost.ghost_name == SPACE_PLATFORM_HUB then
        if ghost.position.x ~= hub.position.x or ghost.position.y ~= hub.position.y then
          return nil, "space platform hub ghost does not match the blueprint position"
        end
        ghost.destroy()
      elseif destroy_preplaced_tile_ghost(ghost) then
        ghost.destroy()
      elseif ghost.type == "tile-ghost" then
        ghost.silent_revive({ raise_revive = false })
      else
        local ghost_name = ghost.ghost_name
        local _, revived, proxy = ghost.silent_revive({ raise_revive = false })
        if revived and revived.valid then
          table.insert(live_entities, revived)
          revive_module_requests(proxy, ghost_name, issues)
        else
          failed[#failed + 1] = { ghost = ghost, name = ghost_name }
        end
      end
    end
  end

  -- Some entities only fit once a later one stands (an elevated rail next to
  -- another elevated rail); retry the failures while a pass still revives some.
  local pending = failed
  while #pending > 0 do
    local still_failed = {}
    for _, attempt in ipairs(pending) do
      -- Not the loop's `_`: Lua 5.4+ makes for-loop variables read-only.
      local collided, revived, proxy
      if attempt.ghost.valid then
        collided, revived, proxy = attempt.ghost.silent_revive({ raise_revive = false })
      end
      if revived and revived.valid then
        table.insert(live_entities, revived)
        revive_module_requests(proxy, attempt.name, issues)
      else
        still_failed[#still_failed + 1] = attempt
      end
    end
    if #still_failed == #pending then
      break
    end
    pending = still_failed
  end
  if #pending > 0 then
    -- One line for diagnosing partly materialised blueprints (docs/platforms.md).
    local by_name = {}
    for _, attempt in ipairs(pending) do
      local entry = by_name[attempt.name] or { count = 0 }
      entry.count = entry.count + 1
      local y = attempt.ghost.valid and attempt.ghost.position.y or nil
      if y then
        entry.min_y = math.min(entry.min_y or y, y)
        entry.max_y = math.max(entry.max_y or y, y)
      end
      by_name[attempt.name] = entry
    end
    local parts = {}
    for name, entry in pairs(by_name) do
      parts[#parts + 1] = string.format("%s=%d(y %s..%s)", name, entry.count,
        tostring(entry.min_y), tostring(entry.max_y))
    end
    table.sort(parts)
    log("Factorio Expo " .. #pending .. " ghosts not revived: " .. table.concat(parts, ", "))
  end
  for _, attempt in ipairs(pending) do
    local prototype = prototypes.entity[attempt.name]
    if not needs_terrain(prototype) and (not prototype or entity_types.is_supported(prototype)) then
      table.insert(issues, issue_record(entity_types.metric_impacts(prototype), "entity_not_materialized", attempt.name))
    end
  end

  if not box then
    return nil, "blueprint footprint could not be determined"
  end

  return {
    box = box,
    footprint = tile_extent(ground),
    entities = live_entities,
    issues = issues,
    build_items = build_items,
  }
end

return blueprint
