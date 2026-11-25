package.path = arg[1] .. "/?.lua;" .. package.path
local platform_hub = require("scripts.platform-hub")
local record = { name = "space-platform-hub", position = { x = 2, y = -1924 },
  quality = "legendary" }
local origin = { x = 0, y = 0 }
local force = {}
local calls = 0
local destroyed = 0
local surface = { platform = {} }
surface.create_entity = function(options)
  calls = calls + 1
  assert(options.position.x == 2 and options.position.y == -1924,
    "hub position changed the centered blueprint placement")
  assert(options.quality == "legendary" and options.force == force)
  local hub = { valid = true, position = options.position,
    quality = { name = options.quality } }
  hub.destroy = function()
    destroyed = destroyed + 1
    hub.valid = false
    surface.platform.hub = nil
  end
  surface.platform.hub = hub
  return hub
end

local hub = platform_hub.place(surface, force, { record }, origin)
assert(hub.valid and calls == 1)
assert(platform_hub.place(surface, force, { record }, origin) == hub and calls == 1)
hub.position = { x = 0, y = 0 }
hub = platform_hub.place(surface, force, { record }, origin)
assert(hub.valid and destroyed == 1 and calls == 2)
hub.quality.name = "normal"
hub = platform_hub.place(surface, force, { record }, origin)
assert(hub.valid and destroyed == 2 and calls == 3)
assert(platform_hub.place({}, force, {}, origin) == nil)

local ok, failure = pcall(platform_hub.place, surface, force, { record, record }, origin)
assert(not ok and tostring(failure):find("multiple hubs", 1, true) and calls == 3)
ok, failure = pcall(platform_hub.place, surface, force, {}, origin)
assert(not ok and tostring(failure):find("missing its hub", 1, true))
surface.platform.hub = nil
surface.create_entity = function() return nil end
ok, failure = pcall(platform_hub.place, surface, force, { record }, origin)
assert(not ok and tostring(failure):find("could not be created", 1, true))

package.loaded["scripts.chunks"] = { generate = function() end }
local blueprint = require("scripts.blueprint")
local payload = { blueprint = { entities = {
  { entity_number = 1, name = record.name, quality = "legendary",
    position = { x = 0, y = 0 } },
}, tiles = { { name = "space-platform-foundation", position = { x = 0, y = 0 } } } } }
helpers = {
  decode_string = function() return "json" end,
  json_to_table = function() return payload end,
  table_to_json = function() return "json" end,
  encode_string = function() return "compressed" end,
}
prototypes = {
  entity = { [record.name] = { type = "space-platform-hub" } },
  tile = { ["space-platform-foundation"] = {} },
}
defines = { build_mode = { superforced = 1 } }
local tiles_placed = false
local inventory_destroyed = false
surface = { platform = {} }
surface.set_tiles = function() tiles_placed = true end
surface.create_entity = function(options)
  assert(tiles_placed, "hub was created before potentially destructive tile placement")
  hub = {
    valid = true, position = options.position, quality = { name = options.quality },
    selection_box = { left_top = { x = -3, y = -3 }, right_bottom = { x = 3, y = 3 } },
  }
  surface.platform.hub = hub
  return hub
end
local return_ghost = false
local stack
stack = {
  set_stack = function() end,
  import_stack = function() return 0 end,
  is_blueprint_setup = function() return true end,
  get_blueprint_entities = function() return payload.blueprint.entities end,
  get_blueprint_tiles = function() return payload.blueprint.tiles end,
  build_blueprint = function(options)
    assert(hub.valid and options.position.x == 0 and options.position.y == 0,
      "ship placement was shifted")
    -- Without absolute snapping at the origin the game moves a hub blueprint
    -- by whole 256-tile cells towards its centre, off its foundation.
    assert(stack.blueprint_absolute_snapping == true
      and stack.blueprint_snap_to_grid.x == 256 and stack.blueprint_snap_to_grid.y == 256
      and stack.blueprint_position_relative_to_grid.x == 0
      and stack.blueprint_position_relative_to_grid.y == 0,
      "platform was not anchored at its blueprint coordinates")
    if not return_ghost then return {} end
    local ghost = {
      valid = true, type = "entity-ghost", ghost_name = record.name,
      position = hub.position, selection_box = hub.selection_box,
      silent_revive = function() error("live hub must not be duplicated") end,
    }
    ghost.destroy = function() ghost.valid = false end
    return { ghost }
  end,
}
game = {
  create_inventory = function()
    return { [1] = stack, destroy = function() inventory_destroyed = true end }
  end,
}
for _, ghost in ipairs({ false, true }) do
  return_ghost = ghost
  tiles_placed = false
  surface.platform.hub = nil
  local placed, render_error, pending = blueprint.render(surface, force, "0exchange")
  assert(placed and not render_error and not pending and inventory_destroyed, tostring(render_error))
  assert(#placed.entities == 1 and placed.entities[1] == hub and #placed.issues == 0,
    "live hub was missing or duplicated in analysis")
  assert(placed.footprint.width == 6 and placed.footprint.height == 6, "footprint covers the hub")
  assert(#placed.build_items == 0, "neither the hub nor foundation without an item costs anything")
end
print("blueprint-position hub creation tests passed")
