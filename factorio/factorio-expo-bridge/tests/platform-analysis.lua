package.path = arg[1] .. "/?.lua;" .. package.path

package.loaded["util"] = {}
script = { active_mods = { base = "2.1.24", ["space-age"] = "2.1.24" } }
local analysis = require("scripts.analysis")

local function thruster(level)
  return {
    prototype = {
      max_performance = { effectivity = 0.51, fluid_usage = 2 },
      fluidbox_prototypes = {
        { filter = { fuel_value = 50000 } },
        { filter = { fuel_value = 50000 } },
      },
    },
    quality = { level = level or 0 },
  }
end

local function calculate(weight_kg, thrusters)
  local surface = {
    platform = weight_kg and { weight = weight_kg } or nil,
    find_entities_filtered = function(filter)
      assert(filter.type == "thruster")
      return thrusters or {}
    end,
  }
  return analysis.calculate({
    job_id = "platform-test",
    force = {},
    surface = surface,
    entities = {},
    footprint = { width = 4, height = 3 },
  })
end

local function close(actual, expected)
  assert(math.abs(actual - expected) <= 1e-9 * math.max(1, expected),
    "expected " .. tostring(expected) .. ", got " .. tostring(actual))
end

for level, multiplier in pairs({ [0] = 1, [1] = 1.3, [2] = 1.6, [3] = 1.9, [5] = 2.5 }) do
  local result = calculate(1000000, { thruster(level) })
  assert(result.status == "succeeded" and result.phase == "analysis")
  assert(result.job_id == "platform-test" and result.schema_version == 1)
  assert(result.footprint.width == 4 and result.footprint.height == 3)
  assert(#result.build_cost.items == 0 and #result.build_cost.raw == 0)
  assert(result.calculator.factorio_version == "2.1.24")
  local platform = result.space_platform
  assert(platform.is_space_platform and platform.weight_tons == 1000)
  close(platform.thrust_mn, 6.12 * multiplier)
  local speed = platform.max_speed_km_s / 60
  local drag = 90 * speed * speed * (1000000 / 200) ^ 0.8 + 10000
  close(drag, 102000 * multiplier / 1.1)
  local count = 0
  for _ in pairs(platform) do count = count + 1 end
  assert(count == 4, "unexpected platform response shape")
end

local engines = {}
for _ = 1, 396 do engines[#engines + 1] = thruster(5) end
local large_ship = calculate(8731400, engines).space_platform
close(large_ship.weight_tons, 8731.4)
close(large_ship.thrust_mn, 6058.8)
close(large_ship.max_speed_km_s, 646.8457374025669)

local heavier_ship = calculate(17462800, engines).space_platform
assert(heavier_ship.max_speed_km_s < large_ship.max_speed_km_s)

assert(calculate(1000000, {}).space_platform.max_speed_km_s == 0)
for _, impulse in ipairs({ 5000, 11000 }) do
  local engine = thruster()
  engine.prototype.max_performance.effectivity = impulse / 200000
  assert(calculate(1000000, { engine }).space_platform.max_speed_km_s == 0,
    "thrust below or at baseline drag must yield zero terminal speed")
end

local custom = thruster()
custom.prototype.fluidbox_prototypes[2].filter.fuel_value = 25000
close(calculate(1000000, { custom }).space_platform.thrust_mn, 4.59)

local non_platform = calculate(nil).space_platform
assert(non_platform.is_space_platform == false)
assert(next(non_platform, "is_space_platform") == nil, "non-platform has platform metrics")
for _, weight in ipairs({ 0, -1 }) do
  local ok, err = pcall(calculate, weight)
  assert(not ok and tostring(err):find("weight must be positive", 1, true))
end

local invalid = thruster()
invalid.prototype.fluidbox_prototypes[1].filter = nil
local ok, err = pcall(calculate, 1000000, { invalid })
assert(not ok and tostring(err):find("fluid energy is unavailable", 1, true))
invalid = thruster()
invalid.prototype.max_performance = nil
ok, err = pcall(calculate, 1000000, { invalid })
assert(not ok and tostring(err):find("maximum performance is unavailable", 1, true))

print("platform analysis tests passed")
