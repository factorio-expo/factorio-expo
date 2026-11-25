--- Capture scheduling for the preview phase. While the job is prepared it finds
--- the capturing client, frames the blueprint and charts the area; on later
--- ticks it waits for the render delay, requests the screenshot, waits for the
--- client to flush it and writes the success marker. Every failure writes the
--- preview failure marker and finishes the phase.
local capture = require("scripts.capture")
local config = require("scripts.config")
local envelope = require("scripts.envelope")
local phase_result = require("scripts.phase-result")
local progress = require("scripts.progress")
local preview_surface = require("scripts.surface")

local preview = {}

local function fail(job, code, message)
  phase_result.write_preview(job, phase_result.failure(job.job_id, "preview", code, message))
  job.preview_done = true
end

--- Readies the preview for a materialized blueprint (bounding box `box` on
--- `surface`). On success the job carries its frame and `preview_ready`.
function preview.prepare(job, surface, box)
  local player, player_error = capture.find_player()
  if not player then
    progress.log(job.job_id, "prepare_job: factorio client unavailable - " .. tostring(player_error))
    fail(job, "factorio_client_unavailable", player_error)
    return
  end
  job.player_index = player.index
  progress.log(job.job_id, "prepare_job: factorio client '" .. player.name .. "' found")

  local frame, frame_error = capture.frame(box, job.space_platform)
  if not frame then
    progress.log(job.job_id, "prepare_job: blueprint too large - " .. tostring(frame_error))
    fail(job, "blueprint_too_large", frame_error)
    return
  end

  local chart_ok, chart_error = pcall(function()
    local chart_area = capture.chart_area(frame)
    preview_surface.generate_capture_area(surface, chart_area)
    player.force.chart(surface, chart_area)
  end)
  if not chart_ok then
    progress.log(job.job_id, "prepare_job: preview preparation failed - " .. tostring(chart_error))
    fail(job, "preview_preparation_failed", tostring(chart_error))
    return
  end
  job.frame = frame
  job.preview_ready = true
  progress.log(job.job_id, "prepare_job: preview frame ready, waiting to capture")
end

local function client_connected(job)
  local player = game.get_player(job.player_index)
  return player ~= nil and player.connected
end

--- Advances a prepared preview by one tick:
--- waiting_for_capture -> capturing -> flushing -> success marker.
--- The screenshot and the success marker are written by the capturing client,
--- so a client that leaves before the marker fails the phase from the server.
function preview.run(job)
  if not job.captured then
    if game.tick < job.capture_tick then
      job.phase = "waiting_for_capture"
      return
    end

    if not client_connected(job) then
      progress.log(job.job_id, "run_preview: factorio client disconnected before capture")
      fail(job, "factorio_client_unavailable", "factorio client disconnected before capture")
      return
    end

    job.phase = "capturing"
    progress.log(job.job_id, "run_preview: requesting capture")
    local captured, capture_error = pcall(capture.take, job)
    if not captured then
      progress.log(job.job_id, "run_preview: capture failed - " .. tostring(capture_error))
      fail(job, "capture_failed", tostring(capture_error))
      return
    end

    job.captured = true
    local pixels = job.frame.resolution.x * job.frame.resolution.y
    local megapixels = math.ceil(pixels / 1000000)
    job.finish_tick = game.tick
      + config.flush_delay_base_ticks
      + megapixels * config.flush_delay_ticks_per_megapixel
    job.phase = "flushing"
    progress.log(job.job_id, "run_preview: capture requested, flushing until tick=" ..
      tostring(job.finish_tick))
    return
  end

  if not client_connected(job) then
    progress.log(job.job_id, "run_preview: factorio client disconnected while flushing")
    fail(job, "factorio_client_unavailable", "factorio client disconnected before the screenshot was written")
    return
  end
  if game.tick < job.finish_tick then
    return
  end

  phase_result.write_preview(job, envelope.success("preview", job.job_id, game.tick, {
    image_path = job.image_path,
    resolution = job.frame.resolution,
    zoom = job.frame.zoom,
    bounding_box = job.bounding_box,
    space_platform = job.space_platform,
  }))
  job.preview_done = true
  progress.log(job.job_id, "run_preview: succeeded, image written to " .. job.image_path)
end

return preview
