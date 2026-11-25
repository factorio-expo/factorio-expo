local blueprint_position = {}

function blueprint_position.center(data)
  local min_x, min_y, max_x, max_y
  for _, records in ipairs({ data.entities or {}, data.tiles or {} }) do
    for _, record in pairs(records) do
      local position = record.position
      if type(position) == "table" and type(position.x) == "number" and type(position.y) == "number" then
        min_x = math.min(min_x or position.x, position.x)
        min_y = math.min(min_y or position.y, position.y)
        max_x = math.max(max_x or position.x, position.x)
        max_y = math.max(max_y or position.y, position.y)
      end
    end
  end
  if not min_x then
    return
  end

  -- Even-tile translations preserve the rail grid as well as entity/tile alignment.
  local offset_x = math.floor((min_x + max_x) / 4) * 2
  local offset_y = math.floor((min_y + max_y) / 4) * 2
  for _, records in ipairs({ data.entities or {}, data.tiles or {} }) do
    for _, record in pairs(records) do
      local position = record.position
      if type(position) == "table" and type(position.x) == "number" and type(position.y) == "number" then
        position.x = position.x - offset_x
        position.y = position.y - offset_y
      end
    end
  end
end

return blueprint_position
