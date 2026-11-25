package.path = arg[1] .. "/?.lua;" .. package.path

local analysis_force = require("scripts.analysis-force")
local unlocked
local research_disabled
local recipes_enabled
local resets = 0
local creations = 0
local force = {
  reset = function()
    resets = resets + 1
    unlocked = {}
    research_disabled = false
    recipes_enabled = false
  end,
  disable_research = function() research_disabled = true end,
  enable_all_recipes = function() recipes_enabled = true end,
  unlock_quality = function(name)
    assert(research_disabled and recipes_enabled, "quality unlocked before force preparation")
    assert(prototypes.quality[name], "unavailable quality unlocked")
    unlocked[name] = true
  end,
}

prototypes = { quality = { normal = {}, uncommon = {}, rare = {}, epic = {}, legendary = {} } }
game = {
  forces = {},
  create_force = function(name)
    assert(name == "factorio-expo-analysis")
    creations = creations + 1
    game.forces[name] = force
    return force
  end,
}

for iteration = 1, 2 do
  assert(analysis_force.prepare() == force)
  assert(resets == iteration, "force was not reset for each job")
  assert(creations == 1, "existing force was not reused")
  for name in pairs(prototypes.quality) do
    assert(unlocked[name], "quality was not unlocked after reset: " .. name)
  end
end

prototypes.quality = { normal = {} }
assert(analysis_force.prepare() == force)
assert(unlocked.normal and not unlocked.legendary, "qualities were not taken from the active content set")

print("analysis force quality tests passed")
