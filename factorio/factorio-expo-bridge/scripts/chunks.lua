local config = require("scripts.config")

local chunks = {}

function chunks.bounds(area, padding)
  local first_x = math.floor(area.left_top.x / 32) - padding
  local first_y = math.floor(area.left_top.y / 32) - padding
  local last_x = math.floor(area.right_bottom.x / 32) + padding
  local last_y = math.floor(area.right_bottom.y / 32) + padding
  local count = (last_x - first_x + 1) * (last_y - first_y + 1)
  if count < 1 or count > config.max_generated_chunks then
    error("blueprint area exceeds the " .. config.max_generated_chunks .. " chunk limit")
  end
  return first_x, first_y, last_x, last_y
end

function chunks.generate(surface, area, padding)
  -- Validate the entire rectangle before submitting any generation requests.
  local first_x, first_y, last_x, last_y = chunks.bounds(area, padding)
  for x = first_x, last_x do
    for y = first_y, last_y do
      surface.request_to_generate_chunks({ x = x * 32 + 16, y = y * 32 + 16 }, 0)
    end
  end
  surface.force_generate_chunk_requests()
end

return chunks
