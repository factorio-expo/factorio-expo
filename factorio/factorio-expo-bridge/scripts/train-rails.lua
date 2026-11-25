-- A blueprint of rolling stock alone has no rails to stand on, so its ghosts
-- cannot be revived. This lays each train out on fresh straight vertical rails
-- (so no diagonal or curved rail is ever needed) and keeps the wagons' order
-- and spacing along the train.
local train_rails = {}

local ROLLING_STOCK = {
  ["locomotive"] = true,
  ["cargo-wagon"] = true,
  ["fluid-wagon"] = true,
  ["artillery-wagon"] = true,
}
-- Wagons of one train are about 7 tiles apart (6 long plus a joint); anything
-- farther away belongs to another train.
local COUPLING_DISTANCE = 8.5
-- Trains are 3 wide: one rail every 4 tiles leaves a tile between them.
local COLUMN_SPACING = 4
-- Rails reach this far past the end wagons (half a wagon, plus some margin).
local RAIL_MARGIN = 4

local function valid_position(entity)
  local position = entity.position
  return type(position) == "table" and type(position.x) == "number"
    and type(position.y) == "number"
end

--- Whether the blueprint holds rolling stock and nothing else.
function train_rails.only_rolling_stock(entities)
  if type(entities) ~= "table" or #entities == 0 then
    return false
  end
  for _, entity in ipairs(entities) do
    if type(entity) ~= "table" or not ROLLING_STOCK[entity.name] or not valid_position(entity) then
      return false
    end
  end
  return true
end

-- Groups the wagons into trains: those within coupling distance of each other.
local function trains_of(entities)
  local group_of = {}
  local groups = {}
  for index, entity in ipairs(entities) do
    local joined
    for other = 1, index - 1 do
      local near = entities[other]
      if math.abs(near.position.x - entity.position.x) <= COUPLING_DISTANCE
          and math.abs(near.position.y - entity.position.y) <= COUPLING_DISTANCE
          and math.sqrt((near.position.x - entity.position.x) ^ 2
            + (near.position.y - entity.position.y) ^ 2) <= COUPLING_DISTANCE then
        local group = group_of[other]
        if not joined then
          joined = group
          table.insert(group, entity)
          group_of[index] = group
        elseif joined ~= group then
          -- The wagon bridges two groups: merge them.
          for _, member in ipairs(group) do
            table.insert(joined, member)
          end
          for key, owner in pairs(group_of) do
            if owner == group then group_of[key] = joined end
          end
          for position, candidate in ipairs(groups) do
            if candidate == group then table.remove(groups, position) break end
          end
        end
      end
    end
    if not joined then
      local group = { entity }
      group_of[index] = group
      groups[#groups + 1] = group
    end
  end
  return groups
end

--- Straightens the trains onto vertical rails (side by side, one rail column
--- each) and adds those rails to `data.entities`.
function train_rails.lay(data)
  local entities = data.entities
  local next_number = 0
  for _, entity in ipairs(entities) do
    if type(entity.entity_number) == "number" then
      next_number = math.max(next_number, entity.entity_number)
    end
  end

  local rails = {}
  for column, group in ipairs(trains_of(entities)) do
    -- Copied: the reference wagon is moved like the others.
    local reference = { position = group[1].position, orientation = group[1].orientation or 0 }
    local angle = 2 * math.pi * reference.orientation
    -- Orientation 0 faces north, a quarter turn faces east.
    local along_x, along_y = math.sin(angle), -math.cos(angle)
    local x = 1 + (column - 1) * COLUMN_SPACING
    local min_y, max_y
    for _, entity in ipairs(group) do
      local along = (entity.position.x - reference.position.x) * along_x
        + (entity.position.y - reference.position.y) * along_y
      local turn = 2 * math.pi * ((entity.orientation or 0) - reference.orientation)
      -- Facing the train's way stays north; facing against it becomes south.
      entity.orientation = math.cos(turn) >= 0 and 0 or 0.5
      -- North is up (negative y), so positions along the train run upwards.
      entity.position = { x = x, y = -along }
      min_y = math.min(min_y or -along, -along)
      max_y = math.max(max_y or -along, -along)
    end
    -- Rails sit on odd coordinates and are two tiles long.
    local first = math.floor((min_y - RAIL_MARGIN - 1) / 2) * 2 + 1
    local last = math.ceil((max_y + RAIL_MARGIN - 1) / 2) * 2 + 1
    for y = first, last, 2 do
      next_number = next_number + 1
      rails[#rails + 1] = {
        entity_number = next_number,
        name = "straight-rail",
        position = { x = x, y = y },
      }
    end
  end
  for _, rail in ipairs(rails) do
    table.insert(entities, rail)
  end
end

return train_rails
