local requests = {}

local function is_module(item_name)
  local prototype = prototypes.item[item_name]
  return prototype and prototype.type == "module"
end

local function insert_plan_item(target, plan)
  local positions = plan.items.in_inventory or {}

  for _, position in pairs(positions) do
    local inventory = target.get_inventory(position.inventory)
    local count = position.count or 1
    local inserted = inventory and inventory.insert({
      name = plan.id.name,
      quality = plan.id.quality or "normal",
      count = count,
    }) or 0

    if inserted ~= count then
      return false
    end
  end

  return true
end

---Fulfils module requests created while reviving a blueprint entity.
function requests.fulfill_modules(proxy)
  if not proxy then
    return true
  end

  local target = proxy.proxy_target
  if not target or not target.valid then
    return false, "module request proxy has no valid target"
  end

  local remaining = {}
  for _, plan in pairs(proxy.insert_plan) do
    if is_module(plan.id.name) then
      if not insert_plan_item(target, plan) then
        return false, "could not insert requested module '" .. plan.id.name .. "' into " .. target.name
      end
    else
      table.insert(remaining, plan)
    end
  end

  if #remaining == 0 then
    proxy.destroy()
  else
    proxy.insert_plan = remaining
  end

  return true
end

return requests
