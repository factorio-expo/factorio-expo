package.path = arg[1] .. "/?.lua;" .. package.path
local chunks = require("scripts.chunks")
local config = require("scripts.config")
local surface = require("scripts.surface")
local requested, forced
local fake = {
  request_to_generate_chunks = function(position, radius)
    assert(radius == 0, "generation must request single chunks")
    local key = math.floor(position.x / 32) .. "," .. math.floor(position.y / 32)
    requested[key] = true
  end,
  force_generate_chunk_requests = function() forced = true end,
}
local function generate(area, padding)
  requested, forced = {}, false
  chunks.generate(fake, area, padding)
  assert(forced)
  local count = 0
  for _ in pairs(requested) do count = count + 1 end
  return count
end
local long = { left_top = { x = -32, y = -34 }, right_bottom = { x = 32, y = 3884 } }
assert(generate(long, 2) == 7 * 128, "long narrow footprint became a square")
assert(requested["-3,-4"] and requested["3,123"])
local offset = { left_top = { x = 100000, y = -200000 }, right_bottom = { x = 100031, y = -199969 } }
assert(generate(offset, 0) == 1, "offset enlarged footprint")
requested, forced = {}, false
surface.generate_capture_area(fake, long)
local count = 0
for _ in pairs(requested) do count = count + 1 end
assert(count == 5 * 126)
requested, forced = {}, false
local oversized = { left_top = { x = 0, y = 0 }, right_bottom = { x = 100000, y = 100000 } }
local ok, err = pcall(chunks.generate, fake, oversized, 2)
assert(not ok and tostring(err):find(tostring(config.max_generated_chunks)))
assert(next(requested) == nil and not forced, "oversized rectangle generated before validation")
print("rectangular bounded chunk tests passed")
