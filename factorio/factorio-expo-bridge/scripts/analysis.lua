-- Headless adaptation of Rate Calculator 3.4.2 (commit
-- 0054c30a05927ac809d691d9ea8d8e0bc00cc44d), Copyright (c) 2020-2023
-- Caleb Heuer. See ../THIRD_PARTY_NOTICES.md.

local entity_types = require("scripts.rate-calculator.entity-types")
local calc_util = require("scripts.rate-calculator.calc-util")
local build_cost = require("scripts.build-cost")
local envelope = require("scripts.envelope")
local json = require("scripts.json")

local analysis = {}

local ENTITY_BLACKLIST = {
  ["buffer-depot"] = true,
  ["fluid-depot"] = true,
  ["fuel-depot"] = true,
  ["request-depot"] = true,
}

local function completeness(unresolved, not_applicable)
  if not_applicable then
    return { status = "not_applicable", unresolved_count = 0 }
  end
  return {
    status = unresolved > 0 and "partial" or "complete",
    unresolved_count = unresolved,
  }
end

local function new_set(force, surface)
  local pollutant_prototype = surface.pollutant_type
  local set = {
    force = force,
    surface = surface,
    pollutant = pollutant_prototype and pollutant_prototype.name or nil,
    rates = {},
    rate_order = {},
    contributors = {},
    power_drain_watts = 0,
    unresolved = {
      materials = 0,
      electricity = 0,
      pollution = 0,
    },
    -- Why entities are unresolved: one tally per (cause, entity name).
    unresolved_causes = {},
    unresolved_cause_order = {},
    -- Per-pollutant breakdown (e.g. "pollution", "spores"), independent of
    -- `pollutant` above (the surface's own tracked pollutant type). Entities
    -- can emit pollutant types the surface doesn't track, e.g. agricultural
    -- towers emit "spores" even on a "pollution" surface.
    pollution_by_type = {},
    pollution_type_order = {},
  }

  --- Records one entity the analysis could not account for: `cause` is one of
  --- the technical causes (docs/production-rate-calculation.md), `metrics` the
  --- metrics it leaves unresolved. Blueprints simply lacking what a machine
  --- needs (no recipe, no fuel, no ore) are not unresolved, only failures of
  --- ours are.
  function set.mark_unresolved(cause, entity_name, metrics)
    local counted = false
    for _, metric in ipairs(metrics) do
      if metric ~= "pollution" or set.pollutant then
        set.unresolved[metric] = set.unresolved[metric] + 1
        counted = true
      end
    end
    if not counted then
      return
    end
    local key = cause .. "|" .. (entity_name or "")
    local entry = set.unresolved_causes[key]
    if not entry then
      entry = { cause = cause, entity = entity_name or "", count = 0 }
      set.unresolved_causes[key] = entry
      table.insert(set.unresolved_cause_order, entry)
    end
    entry.count = entry.count + 1
  end

  return set
end

local function add_pollution_breakdown(set, pollutant_name, amount)
  if amount == 0 then
    return
  end
  local entry = set.pollution_by_type[pollutant_name]
  if not entry then
    entry = { output = 0, input = 0 }
    set.pollution_by_type[pollutant_name] = entry
    table.insert(set.pollution_type_order, pollutant_name)
  end
  if amount > 0 then
    entry.output = entry.output + amount
  else
    entry.input = entry.input - amount
  end
end

local function add_pollution(set, entity, emissions)
  if not set.pollutant then
    return
  end
  local total = 0
  for pollutant_name, amount in pairs(emissions) do
    add_pollution_breakdown(set, pollutant_name, amount)
    total = total + amount
  end
  if total == 0 then
    return
  end
  if total > 0 then
    calc_util.add_rate(
      set,
      "output",
      "item",
      calc_util.POLLUTION,
      "normal",
      total,
      entity.name
    )
  else
    calc_util.add_rate(
      set,
      "input",
      "item",
      calc_util.POLLUTION,
      "normal",
      -total,
      entity.name
    )
  end
end

local function process_entity(set, entity, index)
  if not entity.valid or ENTITY_BLACKLIST[entity.name] or not entity_types.is_supported_type(entity.type) then
    return
  end

  set.current_entity_id = tostring(entity.unit_number or index)

  local emissions = {}
  if set.pollutant and entity.prototype.emissions_per_second then
    -- `EntityPrototype.emissions_per_second` is passive emission (used by
    -- trees/fires), independent of energy sources. Merge every pollutant key
    -- rather than only the one matching the surface's own pollutant type,
    -- for the same reason as the energy-source emissions below.
    calc_util.merge_emissions(emissions, entity.prototype.emissions_per_second)
  end

  local entity_type = entity.type
  if entity_type == "burner-generator" or entity_type == "generator" then
    calc_util.add_rate(
      set,
      "output",
      "item",
      calc_util.POWER,
      "normal",
      entity.prototype.get_max_power_output(entity.quality) * 60,
      entity.name
    )
  elseif entity_type ~= "burner-generator" and entity.prototype.electric_energy_source_prototype then
    emissions = calc_util.process_electric_energy_source(set, entity, emissions)
  elseif entity.prototype.fluid_energy_source_prototype then
    emissions = calc_util.process_fluid_energy_source(set, entity, emissions)
  end

  if entity.burner then
    emissions = calc_util.process_burner(set, entity, emissions)
  end

  if entity_type == "assembling-machine" or entity_type == "furnace" or entity_type == "rocket-silo" then
    emissions = calc_util.process_crafter(set, entity, emissions)
  elseif entity_type == "boiler" then
    calc_util.process_boiler(set, entity)
  elseif entity_type == "generator" then
    calc_util.process_generator(set, entity)
  elseif entity_type == "offshore-pump" then
    calc_util.process_offshore_pump(set, entity)
  elseif entity_type == "reactor" then
    calc_util.process_reactor(set, entity)
  end

  add_pollution(set, entity, emissions)
end

local function process_materialization_issues(set, issues)
  for _, issue in ipairs(issues or {}) do
    local metrics = {}
    if issue.affects_materials ~= false then
      table.insert(metrics, "materials")
    end
    if issue.affects_electricity then
      table.insert(metrics, "electricity")
    end
    if issue.affects_pollution then
      table.insert(metrics, "pollution")
    end
    set.mark_unresolved(issue.cause, issue.entity, metrics)
  end
end

local function rates_for(set, name)
  for _, path in ipairs(set.rate_order) do
    local rate = set.rates[path]
    if rate.name == name then
      return rate
    end
  end
  return {
    input = { rate = 0, machines = 0 },
    output = { rate = 0, machines = 0 },
  }
end

local function public_identity(rate)
  if rate.type == "item" then
    return {
      type = "item",
      name = rate.name,
      quality = rate.quality ~= "" and rate.quality or "normal",
    }
  end
  return {
    type = "fluid",
    name = rate.name,
    quality = json.null,
  }
end

local function material_arrays(set)
  local ingredients = json.array()
  local products = json.array()
  local intermediates = json.array()

  for _, path in ipairs(set.rate_order) do
    local rate = set.rates[path]
    if rate.name ~= calc_util.POWER
      and rate.name ~= calc_util.POLLUTION
      and rate.name ~= calc_util.HEAT then
      local input = rate.input.rate
      local output = rate.output.rate
      local row = public_identity(rate)
      if input > 0 and output > 0 then
        row.input_per_second = input
        row.output_per_second = output
        row.difference_per_second = output - input
        row.producer_count = rate.output.machines
        row.consumer_count = rate.input.machines
        table.insert(intermediates, row)
      elseif input > 0 then
        row.input_per_second = input
        row.consumer_count = rate.input.machines
        table.insert(ingredients, row)
      elseif output > 0 then
        row.output_per_second = output
        row.producer_count = rate.output.machines
        table.insert(products, row)
      end
    end
  end

  return ingredients, products, intermediates
end

local function active_content_set()
  if script.active_mods["space-age"] then
    return "space-age"
  end
  return "base"
end

local function maximum_platform_speed(thrust, weight)
  if weight <= 0 then
    error("space platform weight must be positive")
  end

  local net_thrust = thrust / (1 + weight / 10000000)
  if net_thrust <= 10000 then
    return 0
  end

  -- The expression takes per-tick thrust, mass in kg, and speed in km/tick.
  local drag_coefficient = (weight / 200) ^ 0.8
  return math.sqrt((net_thrust - 10000) / (90 * drag_coefficient)) * 60
end

local function platform_quality_multiplier(level)
  local multipliers = {
    [0] = 1,
    [1] = 1.3,
    [2] = 1.6,
    [3] = 1.9,
    [5] = 2.5,
  }
  local multiplier = multipliers[level]
  if not multiplier then
    error("unsupported thruster quality level " .. tostring(level))
  end
  return multiplier
end

local function space_platform_profile(surface)
  local platform = surface.platform
  if not platform then
    return { is_space_platform = false }
  end

  local thrust = 0
  for _, thruster in ipairs(surface.find_entities_filtered({ type = "thruster" })) do
    local performance = thruster.prototype.max_performance
    if not performance then
      error("thruster maximum performance is unavailable")
    end
    local fuel_energy = 0
    local fuel_boxes = 0
    for _, fluidbox in ipairs(thruster.prototype.fluidbox_prototypes) do
      if not fluidbox.filter or fluidbox.filter.fuel_value <= 0 then
        error("thruster fluid energy is unavailable")
      end
      fuel_energy = fuel_energy + fluidbox.filter.fuel_value
      fuel_boxes = fuel_boxes + 1
    end
    if fuel_boxes ~= 2 then
      error("thruster must have fuel and oxidizer fluid boxes")
    end
    thrust = thrust + performance.effectivity * performance.fluid_usage
      * fuel_energy * platform_quality_multiplier(thruster.quality.level)
  end

  local weight = platform.weight
  return {
    is_space_platform = true,
    weight_tons = weight / 1000,
    -- Factorio 2.1.7+ displays per-second thrust rather than per-tick impulse.
    thrust_mn = thrust * 60 / 1000000,
    max_speed_km_s = maximum_platform_speed(thrust, weight),
  }
end

function analysis.calculate(options)
  local set = new_set(options.force, options.surface)
  process_materialization_issues(set, options.materialization_issues)

  for index, entity in ipairs(options.entities) do
    local ok, failure = pcall(process_entity, set, entity, index)
    if not ok then
      local impacts = entity_types.metric_impacts(entity.valid and entity.prototype or nil)
      local metrics = {}
      if impacts.materials then
        table.insert(metrics, "materials")
      end
      if impacts.electricity then
        table.insert(metrics, "electricity")
      end
      if impacts.pollution then
        table.insert(metrics, "pollution")
      end
      set.mark_unresolved("calculation_error", entity.valid and entity.name or nil, metrics)
      log("Factorio Expo analysis skipped entity after error: " .. tostring(failure))
    end
  end

  local ingredients, products, intermediates = material_arrays(set)
  local electricity = rates_for(set, calc_util.POWER)
  local pollution = rates_for(set, calc_util.POLLUTION)
  local material_completeness = completeness(set.unresolved.materials, false)
  local electricity_completeness = completeness(set.unresolved.electricity, false)
  local pollution_completeness = completeness(set.unresolved.pollution, not set.pollutant)
  table.sort(set.unresolved_cause_order, function(a, b)
    if a.cause ~= b.cause then
      return a.cause < b.cause
    end
    return a.entity < b.entity
  end)
  local unresolved_causes = json.array()
  for _, entry in ipairs(set.unresolved_cause_order) do
    table.insert(unresolved_causes, entry)
  end

  -- `pollutant` above is only the surface's own tracked pollutant type (e.g.
  -- "pollution"). Some entities emit other pollutant types the surface
  -- doesn't track (agricultural towers emit "spores" even on a "pollution"
  -- surface); `by_pollutant` reports every emitted pollutant type
  -- separately so totals aren't silently conflated under one label.
  local by_pollutant = json.array()
  local pollutant_names = {}
  for _, name in ipairs(set.pollution_type_order) do
    table.insert(pollutant_names, name)
  end
  table.sort(pollutant_names)
  for _, name in ipairs(pollutant_names) do
    local entry = set.pollution_by_type[name]
    table.insert(by_pollutant, {
      pollutant = name,
      production_per_second = entry.output,
      absorption_per_second = entry.input,
      net_per_second = entry.output - entry.input,
    })
  end

  return envelope.success("analysis", options.job_id, options.tick or 0, {
    -- Named "state", not "quality", to avoid confusion with Factorio's
    -- quality game mechanic: it reflects unresolved production materials
    -- only, not an item/entity quality tier.
    state = material_completeness.status,
    calculator = {
      implementation = "rate-calculator-derived",
      upstream_version = "3.4.2",
      upstream_commit = "0054c30a05927ac809d691d9ea8d8e0bc00cc44d",
      factorio_version = script.active_mods.base,
      content_set = active_content_set(),
    },
    ingredients = ingredients,
    products = products,
    intermediates = intermediates,
    electricity = {
      consumption_watts = electricity.input.rate,
      production_watts = electricity.output.rate,
      net_watts = electricity.output.rate - electricity.input.rate,
      drain_watts = set.power_drain_watts,
      completeness = electricity_completeness,
    },
    pollution = {
      pollutant = set.pollutant or json.null,
      production_per_second = pollution.output.rate,
      absorption_per_second = pollution.input.rate,
      net_per_second = pollution.output.rate - pollution.input.rate,
      not_applicable = not set.pollutant,
      completeness = pollution_completeness,
      by_pollutant = by_pollutant,
    },
    completeness = {
      materials = material_completeness,
      electricity = electricity_completeness,
      pollution = pollution_completeness,
      unresolved = unresolved_causes,
    },
    space_platform = space_platform_profile(options.surface),
    footprint = { width = options.footprint.width, height = options.footprint.height },
    build_cost = build_cost.section(options.build_items or {}),
  })
end

--- Every peer runs this script and write_file without for_player writes on all
--- of them, so the shared inbox would be written by the server and each client.
--- Player index 0 is the server alone.
function analysis.write_result(path, result)
  helpers.write_file(path, json.encode(result), false, 0)
end

return analysis
