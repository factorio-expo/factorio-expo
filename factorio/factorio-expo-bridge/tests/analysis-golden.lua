-- Encodes representative analysis results with the real json encoder and
-- envelope builder, then compares them with the checked-in golden files that
-- the Go tests (factorio-expo/analysisresult, runner, API) decode.
--
--   lua tests/analysis-golden.lua <modPath>                   check
--   lua tests/analysis-golden.lua <modPath> --write <dir>     regenerate
local mod_path = assert(arg[1], "usage: analysis-golden.lua <modPath> [--write <dir>]")
package.path = mod_path .. "/?.lua;" .. package.path

-- Factorio's helpers.table_to_json is only used for strings by json.lua; the
-- test strings need no escaping.
helpers = {
  table_to_json = function(values)
    return '["' .. values[1] .. '"]'
  end,
}

local json = require("scripts.json")
local envelope = require("scripts.envelope")

local JOB = "pj_golden"
local null = json.null
local array = json.array

local function analysis_fields(overrides)
  local fields = {
    state = "complete",
    calculator = {
      implementation = "rate-calculator-derived",
      upstream_version = "3.4.2",
      factorio_version = "2.0.0",
      content_set = "space-age",
    },
    ingredients = array({
      { type = "fluid", name = "water", quality = null,
        input_per_second = 12.5, consumer_count = 2 },
    }),
    products = array({
      { type = "item", name = "electronic-circuit", quality = "rare",
        output_per_second = 0.5, producer_count = 1 },
    }),
    -- 320/3 consumed, 106.5 produced: the difference needs 16+ digits.
    intermediates = array({
      { type = "item", name = "iron-gear-wheel", quality = "normal",
        input_per_second = 320 / 3, output_per_second = 106.5,
        difference_per_second = 106.5 - 320 / 3, producer_count = 3, consumer_count = 4 },
    }),
    electricity = {
      consumption_watts = 90000, production_watts = 0, net_watts = -90000, drain_watts = 1500,
      completeness = { status = "complete" },
    },
    pollution = {
      pollutant = "pollution", production_per_second = 1.5, absorption_per_second = 0,
      net_per_second = 1.5, not_applicable = false, completeness = { status = "complete" },
      by_pollutant = array(),
    },
    completeness = {
      materials = { status = "complete" },
      electricity = { status = "complete" },
      pollution = { status = "complete" },
      unresolved = array(),
    },
    space_platform = { is_space_platform = false },
    footprint = { width = 12, height = 7 },
    build_cost = {
      items = array({
        { type = "item", name = "assembling-machine-2", quality = "rare", count = 2 },
      }),
      raw = array({
        { type = "item", name = "iron-ore", amount = 47.5 },
        { type = "fluid", name = "water", amount = 1 / 3 },
      }),
    },
  }
  for key, value in pairs(overrides or {}) do
    fields[key] = value
  end
  return fields
end

local fixtures = {
  ["analysis-succeeded.json"] = envelope.success("analysis", JOB, 42, analysis_fields()),
  ["analysis-platform.json"] = envelope.success("analysis", JOB, 42, analysis_fields({
    state = "partial",
    completeness = {
      materials = { status = "partial", unresolved_count = 1 },
      electricity = { status = "complete" },
      pollution = { status = "complete" },
      unresolved = array({ { cause = "unavailable_prototype", entity = "modded-furnace", count = 1 } }),
    },
    ingredients = array(),
    products = array(),
    intermediates = array(),
    space_platform = {
      is_space_platform = true, weight_tons = 1200.5, thrust_mn = 6100, max_speed_km_s = 230.25,
    },
  })),
  ["analysis-failed.json"] = envelope.failure("analysis", JOB, 42, "analysis_failed", "boom"),
}

local function read(path)
  local file = io.open(path, "rb")
  if not file then
    return nil
  end
  local text = file:read("*a")
  file:close()
  return (text:gsub("%s+$", ""))
end

local write_dir = arg[2] == "--write" and assert(arg[3], "--write needs a directory") or nil
local golden_dir = write_dir or (mod_path .. "/../../test-data/analysis")

for name, result in pairs(fixtures) do
  local encoded = json.encode(result)
  local path = golden_dir .. "/" .. name
  if write_dir then
    local file = assert(io.open(path, "wb"))
    file:write(encoded)
    file:close()
  else
    assert(read(path) == encoded, "golden file out of date: " .. name .. "\n" .. encoded)
  end
end

-- Precision: the 14-digit tostring() encoding made output - input drift by more
-- than the Go cross-check allows.
local function round_trips(value)
  return tonumber(json.encode(value)) == value
end
assert(round_trips(320 / 3) and round_trips(106.5 - 320 / 3) and round_trips(0.1) and round_trips(1e21))
assert(json.encode(3) == "3" and json.encode(-2) == "-2" and json.encode(0.5) == "0.5")

print("analysis golden OK")
