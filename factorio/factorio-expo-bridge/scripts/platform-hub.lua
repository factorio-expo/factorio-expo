local platform_hub = {}

function platform_hub.place(surface, force, entities, origin)
  if not surface.platform then return nil end

  local record
  for _, entity in ipairs(entities) do
    if entity.name == "space-platform-hub" then
      if record then error("space platform blueprint contains multiple hubs") end
      record = entity
    end
  end
  if not record then error("space platform blueprint is missing its hub") end

  local position = {
    x = record.position.x + origin.x,
    y = record.position.y + origin.y,
  }
  local quality = record.quality or "normal"
  local hub = surface.platform.hub
  if hub and hub.valid then
    if hub.position.x == position.x and hub.position.y == position.y
        and hub.quality.name == quality then
      return hub
    end
    -- Destroying a hub, unlike killing it, leaves the platform available.
    hub.destroy()
  end
  hub = surface.create_entity({
    name = record.name,
    position = position,
    quality = quality,
    force = force,
    raise_built = false,
  })
  if not hub or not hub.valid then
    error("space platform hub could not be created at the blueprint position")
  end
  return hub
end

return platform_hub
