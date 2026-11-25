local json = {}

local NULL = {}
local ARRAY = {}

json.null = NULL

function json.array(values)
  return setmetatable(values or {}, ARRAY)
end

local function encode_string(value)
  local encoded = helpers.table_to_json({ value })
  return string.sub(encoded, 2, -2)
end

-- tostring() keeps only 14 significant digits in Factorio's Lua, which breaks
-- values the Go side cross-checks (e.g. difference = output - input). Emit
-- integers exactly and every other number with the fewest digits that
-- round-trips.
local function encode_number(value)
  if value == math.floor(value) and math.abs(value) <= 2 ^ 53 then
    return string.format("%d", value)
  end
  for precision = 15, 17 do
    local text = string.format("%." .. precision .. "g", value)
    if tonumber(text) == value then
      return text
    end
  end
  return string.format("%.17g", value)
end

local function encode_value(value)
  if value == NULL then
    return "null"
  end

  local value_type = type(value)
  if value_type == "nil" then
    return "null"
  elseif value_type == "boolean" then
    return value and "true" or "false"
  elseif value_type == "number" then
    if value ~= value or value == math.huge or value == -math.huge then
      error("cannot encode a non-finite number")
    end
    return encode_number(value)
  elseif value_type == "string" then
    return encode_string(value)
  elseif value_type ~= "table" then
    error("cannot encode value of type " .. value_type)
  end

  if getmetatable(value) == ARRAY then
    local parts = {}
    for index = 1, #value do
      parts[index] = encode_value(value[index])
    end
    return "[" .. table.concat(parts, ",") .. "]"
  end

  local keys = {}
  for key in pairs(value) do
    if type(key) ~= "string" then
      error("JSON object keys must be strings")
    end
    table.insert(keys, key)
  end
  table.sort(keys)

  local parts = {}
  for index, key in ipairs(keys) do
    parts[index] = encode_string(key) .. ":" .. encode_value(value[key])
  end
  return "{" .. table.concat(parts, ",") .. "}"
end

function json.encode(value)
  return encode_value(value)
end

return json
