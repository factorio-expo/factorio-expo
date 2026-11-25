local config = require("scripts.config")
local chunks = require("scripts.chunks")

-- Each job renders on its own surface. Reusing one surface is not viable
-- because LuaSurface::clear deletes chunks asynchronously over the following
-- ticks, which would wipe entities built in the same tick. Surfaces from
-- earlier jobs are dropped here; their deletion is also asynchronous but cannot
-- affect the newly created one.
local preview_surface = {}

local function has_prefix(name)
  return string.sub(name, 1, #config.surface_prefix) == config.surface_prefix
end

--- Drops every preview surface. A space platform's surface is named
--- `platform-N`, only the platform itself carries the prefix, so platforms are
--- found through the forces.
local function drop_previous()
  for _, force in pairs(game.forces) do
    for _, platform in pairs(force.platforms or {}) do
      if platform.valid and has_prefix(platform.name) then
        platform.destroy(0)
      end
    end
  end
  for name, surface in pairs(game.surfaces) do
    if has_prefix(name) and not surface.platform and surface.deletable then
      game.delete_surface(surface)
    end
  end
end

preview_surface.drop = drop_previous

local function configure(surface)
  surface.always_day = true
  surface.freeze_daytime = true
  surface.daytime = 0
  surface.show_clouds = false
  surface.ignore_surface_conditions = true
end

local function create_lab_surface(name)
  local surface = game.create_surface(name)
  surface.generate_with_lab_tiles = true
  return surface
end

local function space_platform_location()
  if prototypes.space_location["solar-system-edge"] then
    return "solar-system-edge"
  end
  if prototypes.space_location["space-location-unknown"] then
    return "space-location-unknown"
  end
  return "nauvis"
end

local function move_platform_away_from_planets(platform)
  platform.paused = true

  local location = space_platform_location()
  pcall(function()
    platform.space_location = nil
  end)
  if location then
    pcall(function()
      platform.space_location = location
    end)
  end
end

local function create_space_platform_surface(name, force)
  if not prototypes.item["space-platform-starter-pack"] then
    return nil, "space platform starter pack is not available"
  end

  pcall(force.unlock_space_platforms)
  pcall(function()
    force.unlock_travel_to_space_platforms = true
  end)

  local created, platform = pcall(force.create_space_platform, {
    name = name,
    planet = "nauvis",
    starter_pack = "space-platform-starter-pack",
  })
  if not created then
    return nil, "space platform surface could not be created: " .. platform
  end
  if not platform then
    return nil, "space platform surface could not be created"
  end

  move_platform_away_from_planets(platform)

  if not platform.surface then
    local applied, hub = pcall(platform.apply_starter_pack, true)
    if not applied then
      return nil, "space platform starter pack could not be applied: " .. hub
    end
    if hub and hub.valid then
      hub.destroy()
    end
  end

  if not platform.surface then
    return nil, "space platform surface could not be created"
  end

  platform.surface.generate_with_lab_tiles = false
  return platform.surface
end

function preview_surface.generate_capture_area(surface, area)
  chunks.generate(surface, area, 1)
end

--- Creates an isolated, deterministic stage: lab tiles instead of generated
--- terrain, permanent daylight, and no weather effects.
function preview_surface.create(force, use_space_platform)
  drop_previous()

  -- A monotonic counter keeps names unique even while an older surface is
  -- still being deleted.
  storage.surface_counter = (storage.surface_counter or 0) + 1
  local name = config.surface_prefix .. storage.surface_counter
  local surface, create_error
  if use_space_platform then
    surface, create_error = create_space_platform_surface(name, force)
    if not surface then
      return nil, create_error
    end
    surface.generate_with_lab_tiles = false
  else
    surface = create_lab_surface(name)
  end

  configure(surface)

  return surface
end

return preview_surface
