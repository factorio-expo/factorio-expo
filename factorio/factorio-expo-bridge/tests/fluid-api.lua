-- Boiler, generator and offshore pump rates against the Factorio 2.1.22+ fluid
-- API: fluid filters are fluid prototypes, and temperature limits live on the
-- entity prototype. The mocks raise on unknown keys like Factorio objects do,
-- so reading a removed field (FluidFilter.fluid,
-- LuaFluidBoxPrototype.minimum_temperature) fails the test.
package.path = arg[1] .. "/?.lua;" .. package.path

package.loaded["util"] = {}

local function strict(class, fields, optional)
  optional = optional or {}
  return setmetatable({}, {
    __index = function(_, key)
      if fields[key] ~= nil then
        return fields[key]
      end
      if optional[key] then
        return nil
      end
      error(class .. " doesn't contain key " .. tostring(key) .. ".", 2)
    end,
  })
end

local function fluid(name, default_temperature, heat_capacity)
  return strict("LuaFluidPrototype", {
    name = name,
    default_temperature = default_temperature,
    heat_capacity = heat_capacity,
    object_name = "LuaFluidPrototype",
  })
end

local water = fluid("water", 15, 2000)
local steam = fluid("steam", 15, 200)
prototypes = { fluid = { water = water, steam = steam } }

local calc_util = require("scripts.rate-calculator.calc-util")

local function new_set()
  local set = { rates = {}, rate_order = {}, contributors = {}, current_entity_id = "1",
    unresolved = { materials = 0, pollution = 0 }, causes = {} }
  function set.mark_unresolved(cause, entity_name, metrics)
    for _, metric in ipairs(metrics) do
      set.unresolved[metric] = set.unresolved[metric] + 1
    end
    table.insert(set.causes, cause .. ":" .. entity_name)
  end
  return set
end

-- filters[i] is the filter of fluid storage i; storages beyond #filters do not
-- exist and raise, like LuaEntity does for an invalid index.
local function entity(entity_type, prototype_fields, prototype_optional, filters)
  local function storage(index)
    if index < 1 or index > #filters then
      error("Fluid storage index " .. index .. " out of range", 2)
    end
  end
  return strict("LuaEntity", {
    name = "test-" .. entity_type,
    type = entity_type,
    quality = { name = "normal", level = 0 },
    prototype = strict("LuaEntityPrototype", prototype_fields, prototype_optional),
    get_fluid = function(index)
      storage(index)
      return nil
    end,
    has_fluid_segment = function(index)
      storage(index)
      return false
    end,
    get_fluid_filter = function(index)
      storage(index)
      return filters[index] or nil
    end,
    get_fluid_box_prototype = function(index)
      storage(index)
      return strict("LuaFluidBoxPrototype", { filter = filters[index], object_name = "LuaFluidBoxPrototype" })
    end,
  })
end

-- A fluid's rate key is its name alone: temperatures never split a fluid.
local function rate(set, category, name)
  local found = set.rates["fluid\0" .. name .. "\0"]
  return found and found[category].rate or 0
end

local function close(actual, expected, what)
  assert(math.abs(actual - expected) <= 1e-9 * math.max(1, math.abs(expected)),
    what .. ": expected " .. tostring(expected) .. ", got " .. tostring(actual))
end

local function boiler(mode, minimum_input_temperature, filters)
  return entity("boiler", {
    boiler_mode = mode,
    target_temperature = 165,
    minimum_input_temperature = minimum_input_temperature,
    get_max_energy_usage = function() return 30000 end, -- 1.8 MW
  }, { minimum_input_temperature = true }, filters)
end

-- Vanilla boiler: 1.8 MW heats 15 -> 165 degrees. Water holds 2 kJ per
-- degree and steam 0.2 kJ, so 6 water/s become 60 steam/s (two steam engines).
do
  local set = new_set()
  calc_util.process_boiler(set, boiler("output-to-separate-pipe", nil, { water, steam }))
  close(rate(set, "input", "water"), 6, "boiler water")
  close(rate(set, "output", "steam"), 60, "boiler steam")
  assert(set.unresolved.materials == 0)
end

-- The prototype's minimum input temperature is the cold end of the range.
do
  local set = new_set()
  calc_util.process_boiler(set, boiler("output-to-separate-pipe", 100, { water, steam }))
  close(rate(set, "input", "water"), 30000 / (65 * 2000) * 60, "minimum input water")
  close(rate(set, "output", "steam"), 30000 / (65 * 200) * 60, "minimum input steam")
end

-- heat-fluid-inside has one fluid storage: the same fluid leaves hot.
do
  local set = new_set()
  calc_util.process_boiler(set, boiler("heat-fluid-inside", nil, { water }))
  close(rate(set, "input", "water"), 6, "heat-fluid-inside cold")
  close(rate(set, "output", "water"), 6, "heat-fluid-inside hot")
  assert(set.unresolved.materials == 0)
  assert(#set.rate_order == 1, "cold and hot water are one fluid")
end

-- No filter on the input: unresolved, not an error.
do
  local set = new_set()
  calc_util.process_boiler(set, boiler("output-to-separate-pipe", nil, { false, steam }))
  assert(set.unresolved.materials == 1 and #set.rate_order == 0)
  assert(set.causes[1] == "fluid_unresolved:test-boiler", "the cause is recorded: " .. tostring(set.causes[1]))
end

-- An offshore pump without a filter is not a failure: whether it stands on
-- water, lava or ammonia is not in a blueprint.
do
  local set = new_set()
  calc_util.process_offshore_pump(set, entity("offshore-pump", {
    get_pumping_speed = function() return 20 end,
  }, nil, { false }))
  assert(set.unresolved.materials == 0 and #set.causes == 0 and #set.rate_order == 0)
end

-- Generators and offshore pumps take the fluid from the filter, at its
-- default temperature.
do
  local set = new_set()
  calc_util.process_generator(set, entity("generator", {
    get_fluid_usage_per_tick = function() return 0.5 end,
  }, nil, { steam }))
  close(rate(set, "input", "steam"), 30, "steam engine")

  calc_util.process_offshore_pump(set, entity("offshore-pump", {
    get_pumping_speed = function() return 20 end,
  }, nil, { water }))
  close(rate(set, "output", "water"), 1200, "offshore pump")
  assert(set.unresolved.materials == 0)
end

-- Steam power: the boiler makes steam at 165 degrees, the steam engines take it
-- at the filter's default; both are the one fluid "steam", an intermediate.
do
  local set = new_set()
  calc_util.process_boiler(set, boiler("output-to-separate-pipe", nil, { water, steam }))
  for _ = 1, 2 do
    calc_util.process_generator(set, entity("generator", {
      get_fluid_usage_per_tick = function() return 0.5 end,
    }, nil, { steam }))
  end
  close(rate(set, "output", "steam"), 60, "steam made")
  close(rate(set, "input", "steam"), 60, "steam used")
  assert(#set.rate_order == 2, "water and steam only")
  for _, path in ipairs(set.rate_order) do
    assert(set.rates[path].temperature == nil, "no temperature on a rate")
  end
end

print("fluid API tests passed")
