-- Rolling stock alone is laid out on fresh vertical rails; blueprints with
-- anything else, and empty ones, are left alone.
package.path = arg[1] .. "/?.lua;" .. package.path
local train_rails = require("scripts.train-rails")

assert(not train_rails.only_rolling_stock({}))
assert(not train_rails.only_rolling_stock({
  { entity_number = 1, name = "locomotive", position = { x = 0, y = 0 } },
  { entity_number = 2, name = "assembling-machine-1", position = { x = 9, y = 0 } },
}))

-- A diagonal two-wagon train and a far-away single wagon.
local s = math.sqrt(0.5) * 7
local data = { entities = {
  { entity_number = 1, name = "locomotive", orientation = 0.125, position = { x = 100, y = 100 } },
  { entity_number = 2, name = "cargo-wagon", orientation = 0.125, position = { x = 100 + s, y = 100 - s } },
  { entity_number = 3, name = "cargo-wagon", orientation = 0.625, position = { x = 100 - s, y = 100 + s } },
  { entity_number = 4, name = "fluid-wagon", orientation = 0.5, position = { x = 300, y = 300 } },
} }
assert(train_rails.only_rolling_stock(data.entities))
train_rails.lay(data)

local function near(a, b) return math.abs(a - b) < 1e-6 end
local by_number = {}
local rails = {}
for _, entity in ipairs(data.entities) do
  by_number[entity.entity_number] = entity
  if entity.name == "straight-rail" then rails[#rails + 1] = entity end
end
-- The train keeps its order and 7-tile spacing, straight and vertical.
assert(near(by_number[1].position.x, by_number[2].position.x))
assert(near(by_number[1].position.x, by_number[3].position.x))
assert(near(by_number[1].position.y - by_number[2].position.y, 7))
assert(near(by_number[3].position.y - by_number[1].position.y, 7))
assert(by_number[1].orientation == 0 and by_number[2].orientation == 0)
assert(by_number[3].orientation == 0.5, "a wagon turned around stays turned around")
-- The second train has its own rail column, 4 tiles over.
assert(near(by_number[4].position.x - by_number[1].position.x, 4))
-- Every wagon stands on a rail, rails are unique and sit on odd coordinates.
local seen = {}
for _, rail in ipairs(rails) do
  assert(not seen[rail.entity_number] and rail.entity_number > 4)
  seen[rail.entity_number] = true
  assert(rail.position.x % 2 == 1 and rail.position.y % 2 == 1)
end
for _, wagon in ipairs({ by_number[1], by_number[2], by_number[3], by_number[4] }) do
  local covered = false
  for _, rail in ipairs(rails) do
    if rail.position.x == wagon.position.x and math.abs(rail.position.y - wagon.position.y) <= 1 then
      covered = true
    end
  end
  assert(covered, "wagon without rail below it")
end
print("train rail tests passed")
