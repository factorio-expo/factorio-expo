-- Build cost: the items a blueprint needs and their raw resources through
-- same-named recipes, with loops, net products and unplaceable entities.
package.path = arg[1] .. "/?.lua;" .. package.path
helpers = { table_to_json = function(values) return '["' .. values[1] .. '"]' end }

local function item(name, amount) return { type = "item", name = name, amount = amount } end
local function fluid(name, amount) return { type = "fluid", name = name, amount = amount } end

prototypes = {
  entity = {
    ["assembling-machine-1"] = { items_to_place_this = { { name = "assembling-machine-1", count = 1 } } },
    ["curved-rail-a"] = { items_to_place_this = { { name = "rail", count = 3 } } },
    cliff = {},
  },
  tile = {
    ["stone-path"] = { items_to_place_this = { { name = "stone-brick", count = 1 } } },
    ["out-of-map"] = {},
  },
  item = { ["speed-module"] = {} },
  recipe = {
    ["assembling-machine-1"] = { ingredients = { item("iron-gear-wheel", 5), item("iron-plate", 9) },
      products = { item("assembling-machine-1", 1) } },
    ["iron-gear-wheel"] = { ingredients = { item("iron-plate", 2) }, products = { item("iron-gear-wheel", 1) } },
    ["iron-plate"] = { ingredients = { item("iron-ore", 1) }, products = { item("iron-plate", 1) } },
    rail = { ingredients = { item("stone", 1), item("iron-plate", 1) }, products = { item("rail", 2) } },
    ["stone-brick"] = { ingredients = { item("stone", 2) }, products = { item("stone-brick", 1) } },
    -- Makes 2 per craft, uses 1: nets 1.
    ["speed-module"] = { ingredients = { item("speed-module", 1), fluid("water", 10) },
      products = { item("speed-module", 2) } },
    -- Loop: a <- b <- a. Each stops at the material already being broken down.
    a = { ingredients = { item("b", 1) }, products = { item("a", 1) } },
    b = { ingredients = { item("a", 1) }, products = { item("b", 1) } },
    hidden = { hidden = true, ingredients = { item("iron-ore", 1) }, products = { item("hidden", 1) } },
  },
}

local build_cost = require("scripts.build-cost")

local items = build_cost.items({
  { name = "assembling-machine-1", quality = "rare",
    items = { { id = { name = "speed-module" }, items = { in_inventory = { { inventory = 4, stack = 0 }, { inventory = 4, stack = 1, count = 2 } } } } } },
  { name = "assembling-machine-1" },
  { name = "curved-rail-a" },
  { name = "cliff" },
}, { { name = "stone-path" }, { name = "stone-path" }, { name = "out-of-map" } })

local function find(list, name, quality)
  for _, entry in ipairs(list) do
    if entry.name == name and (quality == nil or entry.quality == quality) then return entry end
  end
end
assert(#items == 5, "items: " .. #items)
assert(find(items, "assembling-machine-1", "normal").count == 1)
assert(find(items, "assembling-machine-1", "rare").count == 1)
assert(find(items, "speed-module", "normal").count == 3, "requests count every inventory slot")
assert(find(items, "rail").count == 3)
assert(find(items, "stone-brick").count == 2)
assert(items[1].name == "assembling-machine-1" and items[1].quality == "normal", "sorted by name and quality")

local raw = build_cost.raw(items)
-- 2 machines x (5 gears x 2 + 9 plates) = 38 ore; 3 rails x 0.5 plate = 1.5 ore.
assert(math.abs(find(raw, "iron-ore").amount - 39.5) < 1e-9, "iron ore " .. find(raw, "iron-ore").amount)
-- 3 rails x 0.5 stone + 2 bricks x 2 stone.
assert(math.abs(find(raw, "stone").amount - 5.5) < 1e-9, "stone " .. find(raw, "stone").amount)
assert(find(raw, "water").type == "fluid" and math.abs(find(raw, "water").amount - 30) < 1e-9, "net products")
assert(raw[1].name == "iron-ore", "largest first")
assert(not find(raw, "iron-plate"), "intermediates are broken down")

local looped = build_cost.raw({ { type = "item", name = "a", quality = "normal", count = 1 } })
assert(#looped == 1 and looped[1].name == "a", "a loop stops at the material it started from")
local hidden = build_cost.raw({ { type = "item", name = "hidden", quality = "normal", count = 4 } })
assert(#hidden == 1 and hidden[1].name == "hidden" and hidden[1].amount == 4, "hidden recipes are not used")

print("build cost tests passed")
