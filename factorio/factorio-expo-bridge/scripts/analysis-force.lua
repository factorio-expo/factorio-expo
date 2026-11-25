local analysis_force = {}

local FORCE_NAME = "factorio-expo-analysis"

function analysis_force.prepare()
  local force = game.forces[FORCE_NAME]
  if not force then
    force = game.create_force(FORCE_NAME)
  end

  force.reset()
  force.disable_research()
  force.enable_all_recipes()
  for name in pairs(prototypes.quality) do
    force.unlock_quality(name)
  end
  return force
end

return analysis_force
