-- Adapted from Rate Calculator 3.4.2 (commit
-- 0054c30a05927ac809d691d9ea8d8e0bc00cc44d), Copyright (c) 2020-2023
-- Caleb Heuer. See ../../THIRD_PARTY_NOTICES.md.
--
-- The GUI, selection history, flib dependency, and unsupported miner/lab
-- inference were removed. The formulas below otherwise retain upstream's raw
-- per-second semantics.

local util = require("util")

local calc_util = {}
local MAX_INT53 = 9007199254740991

local POWER = "__factorio_expo_electricity__"
local POLLUTION = "__factorio_expo_pollution__"
local HEAT = "__factorio_expo_heat__"

calc_util.POWER = POWER
calc_util.POLLUTION = POLLUTION
calc_util.HEAT = HEAT

local function finite_positive(value)
  return type(value) == "number"
    and value == value
    and value > 0
    and value < math.huge
end

-- Energy sources declare emissions per pollutant name (e.g. "pollution",
-- "spores"), but a surface only tracks one pollutant type at a time via
-- `set.pollutant`. Most entities only ever declare the pollutant matching
-- their surface, but agricultural towers emit "spores" even on a "pollution"
-- surface. Emissions are therefore threaded through calculation as a table
-- keyed by pollutant name (rather than a single scalar for `set.pollutant`
-- alone), so no pollutant type is silently dropped, and callers can report
-- a breakdown by source pollutant instead of only a combined total.
local function merge_emissions(into, source, factor)
  factor = factor or 1
  for name, amount in pairs(source or {}) do
    into[name] = (into[name] or 0) + amount * factor
  end
  return into
end
calc_util.merge_emissions = merge_emissions

local function scale_emissions(emissions, factor)
  for name, amount in pairs(emissions) do
    emissions[name] = amount * factor
  end
  return emissions
end
calc_util.scale_emissions = scale_emissions

local function total_emissions(emissions)
  local total = 0
  for _, amount in pairs(emissions) do
    total = total + amount
  end
  return total
end
calc_util.total_emissions = total_emissions

local function material_exists(material_type, name)
  if name == POWER or name == POLLUTION or name == HEAT then
    return true
  elseif material_type == "item" then
    return prototypes.item[name] ~= nil
  elseif material_type == "fluid" then
    return prototypes.fluid[name] ~= nil
  end
  return false
end

local function contributor_key(set, category, path)
  if not set.current_entity_id then
    return nil
  end
  return category .. "\0" .. path .. "\0" .. set.current_entity_id
end

-- A fluid's identity is its name only: machines pick temperatures differently
-- (boilers their target, generators the filter's default, recipes none), so
-- keying by temperature split one fluid into a product and an ingredient.
-- Temperatures still drive the amounts computed below; rates of one fluid at
-- different temperatures are summed.
function calc_util.add_rate(set, category, material_type, name, quality, amount, machine_name)
  if not finite_positive(amount) then
    return
  end
  if not material_exists(material_type, name) then
    set.mark_unresolved("unknown_material", machine_name, { "materials" })
    return
  end

  quality = material_type == "item" and (quality or "normal") or ""
  local path = material_type .. "\0" .. name .. "\0" .. quality
  local rates = set.rates[path]
  if not rates then
    rates = {
      type = material_type,
      name = name,
      quality = quality,
      output = { machines = 0, machine_counts = {}, rate = 0 },
      input = { machines = 0, machine_counts = {}, rate = 0 },
    }
    set.rates[path] = rates
    table.insert(set.rate_order, path)
  end

  local rate = rates[category]
  rate.rate = rate.rate + amount

  -- Upstream increments the machine count for every add_rate call. A recipe
  -- with duplicate product records therefore double-counts one producer. The
  -- contributor guard changes counts only; all expected product amounts are
  -- still accumulated.
  local key = contributor_key(set, category, path)
  if not key or not set.contributors[key] then
    if key then
      set.contributors[key] = true
    end
    rate.machines = rate.machines + 1
    if machine_name then
      rate.machine_counts[machine_name] = (rate.machine_counts[machine_name] or 0) + 1
    end
  end
end

local function normalize_fluid(value)
  if not value then
    return nil
  end
  if type(value) == "string" then
    return prototypes.fluid[value]
  end
  if value.name then
    return prototypes.fluid[value.name] or value
  end
  return nil
end

-- Since Factorio 2.1.22 fluid filters are plain fluid prototypes without a
-- temperature range, so a filter names the fluid only.
local function runtime_fluid(entity, index)
  local fluid = entity.get_fluid(index)
  if fluid then
    return prototypes.fluid[fluid.name], fluid.temperature
  end

  local filter
  local has_segment, segment = pcall(entity.has_fluid_segment, index)
  if has_segment and segment then
    local ok, value = pcall(entity.get_fluid_segment_filter, index)
    if ok then
      filter = value
    end
  end
  if not filter then
    local ok, value = pcall(entity.get_fluid_filter, index)
    if ok then
      filter = value
    end
  end
  return normalize_fluid(filter), nil
end

local function recipe_fluid(entity, index, direction)
  if entity.type ~= "assembling-machine"
    and entity.type ~= "furnace"
    and entity.type ~= "rocket-silo" then
    return nil
  end

  local recipe = entity.get_recipe()
  if not recipe then
    return nil
  end
  local records = direction == "output" and recipe.products or recipe.ingredients
  local matching
  local unique
  local ambiguous = false
  for _, record in ipairs(records) do
    if record.type == "fluid" then
      if not ambiguous then
        if not unique then
          unique = record
        elseif unique.name ~= record.name or unique.temperature ~= record.temperature then
          unique = nil
          ambiguous = true
        end
      end
      if record.fluidbox_index == index then
        if matching then
          return nil
        end
        matching = record
      end
    end
  end
  local record = matching or (not ambiguous and unique or nil)
  if not record then
    return nil
  end
  local fluid = prototypes.fluid[record.name]
  if not fluid then
    return nil
  end
  local temperature = record.temperature
  if not temperature
    and record.minimum_temperature
    and record.maximum_temperature
    and record.minimum_temperature == record.maximum_temperature then
    temperature = record.minimum_temperature
  end
  temperature = temperature or fluid.default_temperature
  return fluid, temperature
end

local function prototype_fluid(entity, index)
  local fluidbox = entity.get_fluid_box_prototype(index)
  if not fluidbox then
    return nil
  end
  if fluidbox.object_name then
    local fluid = normalize_fluid(fluidbox.filter)
    return fluid, fluid and fluid.default_temperature or nil
  end

  local selected
  for _, prototype in ipairs(fluidbox) do
    local fluid = normalize_fluid(prototype.filter)
    if fluid then
      if selected and selected.name ~= fluid.name then
        return nil
      end
      selected = fluid
    end
  end
  return selected, selected and selected.default_temperature or nil
end

-- Blank analysis surfaces contain no fluids. Prefer live state, then recipe
-- and runtime filters, and finally one unambiguous prototype default.
function calc_util.infer_fluid(entity, index, direction)
  local fluid, temperature = runtime_fluid(entity, index)
  if fluid then
    return fluid, temperature or fluid.default_temperature
  end
  fluid, temperature = recipe_fluid(entity, index, direction)
  if fluid then
    return fluid, temperature
  end
  return prototype_fluid(entity, index)
end

function calc_util.process_burner(set, entity, emissions)
  local entity_prototype = entity.prototype
  local burner_prototype = entity_prototype.burner_prototype
  local burner = entity.burner
  local currently_burning = burner.currently_burning

  if not currently_burning then
    local item = burner.inventory.get_contents()[1]
    if item then
      currently_burning = {
        name = prototypes.item[item.name],
        quality = prototypes.quality[item.quality],
      }
    end
  end
  local max_energy_usage = entity_prototype.get_max_energy_usage(entity.quality) * (entity.consumption_bonus + 1)
  if not currently_burning then
    -- A blueprint never carries fuel, so no fuel rate is invented. Pollution is
    -- the full-power figure of the wiki's table: the fuel emissions multiplier
    -- is 1, as it is for wood and coal.
    if not set.pollutant then
      return emissions
    end
    return merge_emissions(emissions, burner_prototype.emissions_per_joule, 60 * max_energy_usage)
  end

  local fuel = currently_burning.name
  local burns_per_second = 1 / (fuel.fuel_value / (max_energy_usage / burner_prototype.effectivity) / 60)
  calc_util.add_rate(
    set,
    "input",
    "item",
    fuel.name,
    currently_burning.quality.name,
    burns_per_second,
    entity.name
  )

  local burnt_result = fuel.burnt_result
  if burnt_result then
    calc_util.add_rate(
      set,
      "output",
      "item",
      burnt_result.name,
      currently_burning.quality.name,
      burns_per_second,
      entity.name
    )
  end

  if not set.pollutant then
    return emissions
  end
  return merge_emissions(
    emissions,
    burner_prototype.emissions_per_joule,
    60 * max_energy_usage * fuel.fuel_emissions_multiplier
  )
end

function calc_util.process_boiler(set, entity)
  local entity_prototype = entity.prototype
  local input_fluid, input_temperature = calc_util.infer_fluid(entity, 1, "input")
  if not input_fluid then
    set.mark_unresolved("fluid_unresolved", entity.name, { "materials" })
    return
  end

  -- Each side's amount is the energy over its own heat capacity: in 2.1 water
  -- holds 2 kJ per degree and steam 0.2 kJ, so one water becomes ten steam.
  local max_energy_usage = entity_prototype.get_max_energy_usage(entity.quality)
  local function amount(fluid, from_temperature)
    local energy_per_amount = (entity_prototype.target_temperature - from_temperature) * fluid.heat_capacity
    if not finite_positive(energy_per_amount) then
      return nil
    end
    return max_energy_usage / energy_per_amount * 60
  end

  local minimum_temperature = entity_prototype.minimum_input_temperature
    or input_temperature
    or input_fluid.default_temperature
  local fluid_usage = amount(input_fluid, minimum_temperature)
  if not fluid_usage then
    set.mark_unresolved("invalid_value", entity.name, { "materials" })
    return
  end
  calc_util.add_rate(
    set,
    "input",
    "fluid",
    input_fluid.name,
    nil,
    fluid_usage,
    entity.name
  )

  -- A heat-fluid-inside boiler heats the fluid flowing through its one
  -- fluidbox; it is reported as cold fluid in and hot fluid out. Upstream
  -- 3.4.2 reports no fluid rates for this mode.
  local output_fluid, output_amount = input_fluid, fluid_usage
  if entity_prototype.boiler_mode ~= "heat-fluid-inside" then
    output_fluid = calc_util.infer_fluid(entity, 2, "output")
    output_amount = output_fluid
      and amount(output_fluid, entity_prototype.minimum_input_temperature or output_fluid.default_temperature)
  end
  if not output_fluid then
    set.mark_unresolved("fluid_unresolved", entity.name, { "materials" })
    return
  end
  if not output_amount then
    set.mark_unresolved("invalid_value", entity.name, { "materials" })
    return
  end

  calc_util.add_rate(set, "output", "fluid", output_fluid.name, nil, output_amount, entity.name)
end

function calc_util.process_crafter(set, entity, emissions)
  local recipe, quality = entity.get_recipe()
  if not recipe and entity.type == "furnace" then
    local previous = entity.previous_recipe
    if previous then
      recipe = set.force.recipes[previous.name.name]
      quality = previous.quality
    end
  end
  if not recipe then
    -- A blueprint carries no recipe for furnaces, and machines may have none
    -- set: no rates are invented, pollution assumes a recipe multiplier of 1.
    return scale_emissions(emissions, 1 + entity.pollution_bonus)
  end

  quality = quality or prototypes.quality.normal
  local recipe_duration = recipe.energy / entity.crafting_speed
  if not finite_positive(recipe_duration) then
    set.mark_unresolved("invalid_value", entity.name, { "materials" })
    return emissions
  end

  for _, ingredient in ipairs(recipe.ingredients) do
    calc_util.add_rate(
      set,
      "input",
      ingredient.type,
      ingredient.name,
      ingredient.type == "item" and quality.name or nil,
      ingredient.amount / recipe_duration,
      entity.name
    )
  end

  local productivity = 1
    + math.min(entity.productivity_bonus + recipe.productivity_bonus, recipe.prototype.maximum_productivity)
  for _, product in ipairs(recipe.products) do
    if product.type ~= "research-progress" then
      local expected_amount = util.product_amount(product)
      local ignored = math.min(expected_amount, product.ignored_by_productivity or 0)
      local amount = (ignored + (expected_amount - ignored) * productivity) / recipe_duration
      calc_util.add_rate(
        set,
        "output",
        product.type,
        product.name,
        product.type == "item" and quality.name or nil,
        amount,
        entity.name
      )
    end
  end

  return scale_emissions(emissions, recipe.prototype.emissions_multiplier * (1 + entity.pollution_bonus))
end

function calc_util.process_electric_energy_source(set, entity, emissions)
  local entity_prototype = entity.prototype
  if entity.type == "electric-energy-interface" then
    local production = entity.power_production * 60
    local usage = entity.power_usage * 60
    calc_util.add_rate(set, "output", "item", POWER, "normal", production, entity.name)
    calc_util.add_rate(set, "input", "item", POWER, "normal", usage, entity.name)
    return emissions
  end

  local source = entity_prototype.electric_energy_source_prototype
  local max_usage = entity_prototype.get_max_energy_usage(entity.quality) or 0
  if max_usage > 0 and max_usage < MAX_INT53 then
    local consumption_bonus = entity.consumption_bonus + 1
    local drain = source.drain
    local amount = max_usage * consumption_bonus
    if max_usage ~= drain then
      amount = amount + drain
    end
    calc_util.add_rate(set, "input", "item", POWER, "normal", amount * 60, entity.name)
    if drain > 0 then
      set.power_drain_watts = set.power_drain_watts + drain * 60
    end
    if set.pollutant then
      merge_emissions(emissions, source.emissions_per_joule, max_usage * consumption_bonus * 60)
    end
  end

  local max_production = entity_prototype.get_max_energy_production(entity.quality)
  if max_production > 0 and max_production < MAX_INT53 then
    if entity.type == "solar-panel" then
      max_production = max_production
        * entity.surface.solar_power_multiplier
        * entity.surface.get_property("solar-power")
        / prototypes.surface_property["solar-power"].default_value
    end
    calc_util.add_rate(set, "output", "item", POWER, "normal", max_production * 60, entity.name)
  end
  return emissions
end

function calc_util.process_fluid_energy_source(set, entity, emissions)
  local entity_prototype = entity.prototype
  local source = entity_prototype.fluid_energy_source_prototype
  local index = entity.type == "boiler" and entity.fluids_count or 1
  local fluid, temperature = calc_util.infer_fluid(entity, index, "input")
  if not fluid then
    set.mark_unresolved("fluid_unresolved", entity.name, { "materials", "pollution" })
    return emissions
  end

  local max_usage = entity_prototype.get_max_energy_usage(entity.quality) * (entity.consumption_bonus + 1)
  local value
  if source.scale_fluid_usage then
    if source.burns_fluid and fluid.fuel_value > 0 then
      value = max_usage / (fluid.fuel_value / 60) / source.effectivity
    else
      local temperature_value = temperature - fluid.default_temperature
      if temperature_value > 0 then
        value = max_usage / (temperature_value * fluid.heat_capacity) / source.effectivity * 60
      end
    end
  else
    value = source.fluid_usage_per_tick / source.effectivity * 60
  end
  if not finite_positive(value) then
    set.mark_unresolved("invalid_value", entity.name, { "materials", "pollution" })
    return emissions
  end

  calc_util.add_rate(set, "input", "fluid", fluid.name, nil, value, entity.name)
  if not set.pollutant then
    return emissions
  end
  return merge_emissions(emissions, source.emissions_per_joule, max_usage * 60)
end

function calc_util.process_generator(set, entity)
  local fluid = calc_util.infer_fluid(entity, 1, "input")
  if not fluid then
    set.mark_unresolved("fluid_unresolved", entity.name, { "materials" })
    return
  end
  calc_util.add_rate(
    set,
    "input",
    "fluid",
    fluid.name,
    nil,
    entity.prototype.get_fluid_usage_per_tick(entity.quality) * 60,
    entity.name
  )
end

function calc_util.process_offshore_pump(set, entity)
  local fluid = calc_util.infer_fluid(entity, 1, "output")
  if not fluid then
    -- Whether the pump stands on water, lava or ammonia is not in a blueprint:
    -- its output is unknown, not a failure.
    return
  end
  calc_util.add_rate(
    set,
    "output",
    "fluid",
    fluid.name,
    nil,
    entity.prototype.get_pumping_speed(entity.quality) * 60,
    entity.name
  )
end

function calc_util.process_reactor(set, entity)
  calc_util.add_rate(
    set,
    "output",
    "item",
    HEAT,
    "normal",
    entity.prototype.get_max_energy_usage(entity.quality)
      * (1 + entity.neighbour_bonus)
      * (1 + entity.consumption_bonus)
      * 60,
    entity.name
  )
end

return calc_util
