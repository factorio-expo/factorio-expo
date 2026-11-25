if data.raw["unit-spawner"]["gleba-spawner"] and data.raw["unit-spawner"]["gleba-spawner-small"] then
    data.raw["unit-spawner"]["gleba-spawner-small"].collision_mask = table.deepcopy(data.raw["unit-spawner"]["gleba-spawner"].collision_mask)
end