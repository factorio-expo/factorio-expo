--- Build cost of a blueprint: the items needed to place it (entities, tiles,
--- item requests such as modules) and those items broken down into raw
--- resources through the pinned game's recipes.
local json = require("scripts.json")

local build_cost = {}

-- Deeper recipe chains are cut and their item counted as raw.
local MAX_DEPTH = 32

local function placing_item(prototype)
  local item = prototype and prototype.items_to_place_this and prototype.items_to_place_this[1]
  if not item then
    return nil
  end
  return item.name, item.count or 1
end

local function request_count(plan)
  local items = plan.items or {}
  local count = items.grid_count or 0
  for _, position in pairs(items.in_inventory or {}) do
    count = count + (position.count or 1)
  end
  return count
end

local function sorted(values, less)
  local list = {}
  for _, value in pairs(values) do
    list[#list + 1] = value
  end
  table.sort(list, less)
  return list
end

--- The items a blueprint needs, from its entity records and tiles: per item
--- and quality, sorted by name then quality. Entities and tiles that no item
--- places (cliffs, the platform hub) cost nothing.
function build_cost.items(records, tiles)
  local totals = {}
  local function add(name, quality, count)
    if count <= 0 then
      return
    end
    local key = name .. "\0" .. quality
    local entry = totals[key]
    if not entry then
      entry = { type = "item", name = name, quality = quality, count = 0 }
      totals[key] = entry
    end
    entry.count = entry.count + count
  end

  for _, record in pairs(records or {}) do
    local name, count = placing_item(prototypes.entity[record.name])
    if name then
      add(name, record.quality or "normal", count)
    end
    for _, plan in pairs(record.items or {}) do
      local id = plan.id or {}
      if id.name and prototypes.item[id.name] then
        add(id.name, id.quality or "normal", request_count(plan))
      end
    end
  end
  for _, tile in pairs(tiles or {}) do
    local name, count = placing_item(prototypes.tile[tile.name])
    if name then
      add(name, "normal", count)
    end
  end

  return sorted(totals, function(a, b)
    if a.name ~= b.name then
      return a.name < b.name
    end
    return a.quality < b.quality
  end)
end

local function product_amount(product)
  local amount = product.amount
  if amount == nil then
    amount = ((product.amount_min or 0) + (product.amount_max or 0)) / 2
  end
  return amount * (product.probability or 1) + (product.extra_count_fraction or 0)
end

--- The recipe that makes a material: the visible recipe of the same name, and
--- how many of the material one craft makes net of the same material it
--- consumes. Anything else (no such recipe, nothing made) is raw.
local function recipe_for(material_type, name)
  local recipe = prototypes.recipe[name]
  if not recipe or recipe.hidden or recipe.parameter then
    return nil
  end
  local made = 0
  for _, product in pairs(recipe.products or {}) do
    if product.type == material_type and product.name == name then
      made = made + product_amount(product)
    end
  end
  for _, ingredient in pairs(recipe.ingredients or {}) do
    if ingredient.type == material_type and ingredient.name == name then
      made = made - ingredient.amount
    end
  end
  if made <= 0 or #(recipe.ingredients or {}) == 0 then
    return nil
  end
  return recipe, made
end

--- The raw materials of one unit of a material, keyed by type and name.
local function breakdown(material_type, name, memo, visiting, depth)
  local key = material_type .. "\0" .. name
  if memo[key] then
    return memo[key]
  end
  local recipe, made
  if not visiting[key] and depth < MAX_DEPTH then
    recipe, made = recipe_for(material_type, name)
  end
  if not recipe then
    -- A material inside a recipe loop is raw for this chain only.
    local raw = { [key] = { type = material_type, name = name, amount = 1 } }
    if not visiting[key] then
      memo[key] = raw
    end
    return raw
  end

  visiting[key] = true
  local result = {}
  for _, ingredient in pairs(recipe.ingredients) do
    local per_unit = ingredient.amount / made
    for raw_key, raw in pairs(breakdown(ingredient.type, ingredient.name, memo, visiting, depth + 1)) do
      local entry = result[raw_key]
      if not entry then
        entry = { type = raw.type, name = raw.name, amount = 0 }
        result[raw_key] = entry
      end
      entry.amount = entry.amount + raw.amount * per_unit
    end
  end
  visiting[key] = nil
  memo[key] = result
  return result
end

--- The raw resources behind a list of build items, at normal quality (quality
--- and productivity are ignored), largest amount first.
function build_cost.raw(items)
  local memo = {}
  local totals = {}
  for _, item in ipairs(items) do
    for key, raw in pairs(breakdown("item", item.name, memo, {}, 0)) do
      local entry = totals[key]
      if not entry then
        entry = { type = raw.type, name = raw.name, amount = 0 }
        totals[key] = entry
      end
      entry.amount = entry.amount + raw.amount * item.count
    end
  end
  return sorted(totals, function(a, b)
    if a.amount ~= b.amount then
      return a.amount > b.amount
    end
    if a.type ~= b.type then
      return a.type < b.type
    end
    return a.name < b.name
  end)
end

--- The build_cost section of the analysis result.
function build_cost.section(items)
  return {
    items = json.array(items),
    raw = json.array(build_cost.raw(items)),
  }
end

return build_cost
