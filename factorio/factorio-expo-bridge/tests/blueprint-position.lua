package.path = arg[1] .. "/?.lua;" .. package.path
local blueprint_position = require("scripts.blueprint-position")

local data = {
  entities = {
    { entity_number = 1, name = "electromagnetic-plant", position = { x = 567, y = -5.5 },
      quality = "legendary", recipe = "supercapacitor", recipe_quality = "epic" },
    { entity_number = 2, position = { x = 635.5, y = 59.5 } },
  },
  tiles = { { name = "space-platform-foundation", position = { x = 566, y = -6 } } },
  wires = { { 1, 5, 2, 5 } },
}
blueprint_position.center(data)
assert(data.entities[1].position.x == -33 and data.entities[1].position.y == -31.5)
assert(data.entities[2].position.x == 35.5 and data.entities[2].position.y == 33.5)
assert(data.tiles[1].position.x == -34 and data.tiles[1].position.y == -32)
assert(data.entities[2].position.x - data.entities[1].position.x == 68.5)
assert(data.entities[1].quality == "legendary" and data.entities[1].recipe_quality == "epic")
assert(data.entities[1].recipe == "supercapacitor" and data.wires[1][3] == 2)

local tiles = { tiles = { { position = { x = -10001, y = 20001 } } } }
blueprint_position.center(tiles)
assert(tiles.tiles[1].position.x == 1 and tiles.tiles[1].position.y == 1)
blueprint_position.center({})
blueprint_position.center({ entities = {}, tiles = {} })
print("blueprint coordinate normalization tests passed")
