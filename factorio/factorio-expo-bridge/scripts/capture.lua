local config = require("scripts.config")
local json = require("scripts.json")

-- Screenshots are only ever produced by a connected graphical client. The
-- headless server ignores take_screenshot, so every capture is addressed to one
-- specific player via by_player, and the success marker is written for that same
-- player index.
local capture = {}

--- Resolves the client that will render the screenshot. The bridge only ever
--- runs on a private, single-tenant network, so any connected player is
--- assumed to be the factorio client -- there is no need to match a name.
function capture.find_player()
  for _, player in pairs(game.connected_players) do
    return player
  end

  return nil, "no factorio client is connected"
end

--- Rewrites the heartbeat file through the capturing client. Only that client
--- can write it, so the runner sees it go stale when the client dies, which the
--- server only notices after its peer timeout.
function capture.write_heartbeat()
  if game.tick % config.heartbeat_interval_ticks ~= 0 then
    return
  end
  local player = capture.find_player()
  if player then
    helpers.write_file(config.output_dir .. "/" .. config.client_heartbeat_file, tostring(game.tick), false, player.index)
  end
end

--- Derives camera position, zoom and image size that fit the blueprint.
function capture.frame(box, use_space_platform)
  local margin = config.margin_tiles
  local width = math.max((box.right_bottom.x - box.left_top.x) + margin * 2, 1)
  local height = math.max((box.right_bottom.y - box.left_top.y) + margin * 2, 1)

  local fit_zoom = math.min(
    config.max_resolution / (width * config.pixels_per_tile),
    config.max_resolution / (height * config.pixels_per_tile),
    config.max_zoom
  )
  if fit_zoom < config.min_zoom then
    return nil, "blueprint is too large for the configured preview resolution"
  end
  local zoom = fit_zoom

  local resolution = {
    x = math.min(math.ceil(width * config.pixels_per_tile * zoom), config.max_resolution),
    y = math.min(math.ceil(height * config.pixels_per_tile * zoom), config.max_resolution),
  }

  local anti_alias = math.max(resolution.x, resolution.y) <= config.max_anti_alias_resolution
  if use_space_platform then
    anti_alias = false
  end

  return {
    position = {
      x = (box.left_top.x + box.right_bottom.x) / 2,
      y = (box.left_top.y + box.right_bottom.y) / 2,
    },
    resolution = resolution,
    zoom = zoom,
    anti_alias = anti_alias,
    width_tiles = width,
    height_tiles = height,
  }
end

--- Area the capturing force must have charted for the scene to render unfogged.
function capture.chart_area(frame)
  local half_width = frame.width_tiles / 2
  local half_height = frame.height_tiles / 2
  return {
    left_top = { x = frame.position.x - half_width, y = frame.position.y - half_height },
    right_bottom = { x = frame.position.x + half_width, y = frame.position.y + half_height },
  }
end

function capture.take(job)
  game.take_screenshot({
    by_player = job.player_index,
    surface = job.surface_name,
    position = job.frame.position,
    resolution = job.frame.resolution,
    zoom = job.frame.zoom,
    path = job.image_path,
    show_gui = false,
    show_entity_info = true,
    show_cursor_building_preview = false,
    anti_alias = job.frame.anti_alias,
    hide_clouds = true,
    hide_fog = true,
    daytime = 0,
    water_tick = 0,
    force_render = true,
    allow_in_replay = false,
  })
end

--- Writes the job-scoped completion marker the runner polls for. A success
--- marker is written for the factorio client, next to the image it just
--- produced. A failure marker is written by the server (player index 0): the
--- client may be the thing that went away, and a write addressed to a
--- disconnected player never lands.
function capture.write_result(job, result)
  local for_player = result.status == "succeeded" and job.player_index or 0
  helpers.write_file(job.preview_result_path, json.encode(result), false, for_player)
end

return capture
