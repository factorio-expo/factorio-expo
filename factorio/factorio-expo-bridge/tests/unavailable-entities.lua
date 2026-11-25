-- Entities whose prototype is unavailable are swapped for a display panel before
-- import. They must still be reported as issues so the result is not "complete".
package.path = arg[1] .. "/?.lua;" .. package.path

local imported_json
local stack = {
  set_stack = function() end,
  import_stack = function() return 0 end,
  is_blueprint_setup = function() return true end,
  get_blueprint_entities = function() return {} end,
  get_blueprint_tiles = function() return {} end,
  build_blueprint = function() return { { valid = true, type = "entity-ghost", ghost_name = "known",
    position = { x = 0, y = 0 }, selection_box = { left_top = { x = 0, y = 0 }, right_bottom = { x = 1, y = 1 } },
    silent_revive = function() return {}, { valid = true } end } } end,
}
local payload = { blueprint = { entities = {
  { entity_number = 1, name = "known", position = { x = 0, y = 0 } },
  { entity_number = 2, name = "modded-furnace", position = { x = 2, y = 0 } },
  { entity_number = 3, name = "modded-furnace", position = { x = 4, y = 0 } },
} } }
prototypes = {
  entity = { known = { type = "container" } },
  recipe = {}, tile = {},
}
helpers = {
  decode_string = function() return "json" end,
  json_to_table = function() return payload end,
  table_to_json = function(value) imported_json = value; return "{}" end,
  encode_string = function() return "data" end,
}
game = { create_inventory = function() return { [1] = stack, destroy = function() end } end }
defines = { build_mode = { superforced = 0 } }
log = function() end
package.loaded["scripts.chunks"] = { generate = function() end }
package.loaded["scripts.platform-hub"] = { place = function() return nil end }
package.loaded["scripts.requests"] = { fulfill_modules = function() return true end }

local blueprint = require("scripts.blueprint")
local surface = { name = "s" }
local force = { name = "f" }
local placed, err = blueprint.render(surface, force, "0abc")
assert(placed, err)
local issues = placed.issues
assert(placed.footprint.width == 1 and placed.footprint.height == 1,
  "footprint covers the placed ghost")

assert(#issues == 2, "each replaced entity must be an issue, got " .. #issues)
assert(issues[1].affects_materials and issues[1].affects_electricity
  and issues[1].affects_pollution, "an unknown entity may affect every metric")
assert(issues[1].cause == "unavailable_prototype" and issues[1].entity == "modded-furnace",
  "an issue names its cause and entity")

print("unavailable entity tests passed")

-- An empty blueprint renders as bare ground: no entities, no failure.
stack.is_blueprint_setup = function() return false end
local empty, empty_err = blueprint.render(surface, force, "0abc")
assert(empty, empty_err)
assert(#empty.entities == 0 and #empty.issues == 0, "an empty blueprint places nothing")
assert(empty.footprint.width >= 1 and empty.footprint.height >= 1)
assert(empty.box.right_bottom.x > empty.box.left_top.x)
assert(blueprint.needs_space_platform("0abc") == false)
print("empty blueprint tests passed")
