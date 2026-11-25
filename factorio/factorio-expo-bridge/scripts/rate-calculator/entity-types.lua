-- Entity types accepted by Rate Calculator 3.4.2's selection tool.
local supported = {
  ["accumulator"] = true,
  ["agricultural-tower"] = true,
  ["ammo-turret"] = true,
  ["arithmetic-combinator"] = true,
  ["artillery-turret"] = true,
  ["assembling-machine"] = true,
  ["beacon"] = true,
  ["boiler"] = true,
  ["burner-generator"] = true,
  ["constant-combinator"] = true,
  ["decider-combinator"] = true,
  ["electric-energy-interface"] = true,
  ["electric-turret"] = true,
  ["fluid-turret"] = true,
  ["furnace"] = true,
  ["generator"] = true,
  ["heat-interface"] = true,
  ["inserter"] = true,
  ["lab"] = true,
  ["lamp"] = true,
  ["loader"] = true,
  ["loader-1x1"] = true,
  ["locomotive"] = true,
  ["mining-drill"] = true,
  ["offshore-pump"] = true,
  ["programmable-speaker"] = true,
  ["pump"] = true,
  ["radar"] = true,
  ["reactor"] = true,
  ["roboport"] = true,
  ["rocket-silo"] = true,
  ["solar-panel"] = true,
  ["turret"] = true,
}

local material_entity_types = {
  ["assembling-machine"] = true,
  ["beacon"] = true,
  ["boiler"] = true,
  ["burner-generator"] = true,
  ["furnace"] = true,
  ["generator"] = true,
  ["lab"] = true,
  ["mining-drill"] = true,
  ["offshore-pump"] = true,
  ["rocket-silo"] = true,
}

return {
  is_supported = function(prototype)
    return prototype and supported[prototype.type] == true
  end,
  is_supported_type = function(entity_type)
    return supported[entity_type] == true
  end,
  metric_impacts = function(prototype)
    if not prototype then
      return {
        materials = true,
        electricity = true,
        pollution = true,
      }
    end
    return {
      materials = material_entity_types[prototype.type] == true
        or prototype.burner_prototype ~= nil
        or prototype.fluid_energy_source_prototype ~= nil,
      electricity = prototype.type == "generator"
        or prototype.type == "burner-generator"
        or prototype.electric_energy_source_prototype ~= nil,
      pollution = prototype.emissions_per_second ~= nil
        or prototype.burner_prototype ~= nil
        or prototype.fluid_energy_source_prototype ~= nil,
    }
  end,
}
