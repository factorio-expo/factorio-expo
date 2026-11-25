--- The active job (storage.job): its record, the accepted/status payloads, and
--- the tick state machine. Phases: preparing -> materializing -> stabilizing,
--- then analysis (analyzing) and the preview schedule (waiting_for_capture ->
--- capturing -> flushing) run until both requested phases are done, when the
--- job moves to storage.completed_job.
local analysis = require("scripts.analysis")
local analysis_force = require("scripts.analysis-force")
local blueprint = require("scripts.blueprint")
local capture = require("scripts.capture")
local config = require("scripts.config")
local phase_result = require("scripts.phase-result")
local preview = require("scripts.preview-schedule")
local preview_surface = require("scripts.surface")
local progress = require("scripts.progress")

local job = {}

local function phase_paths(job_id)
  local directory = config.output_dir .. "/" .. job_id
  return {
    image = directory .. "/preview.png",
    preview = directory .. "/preview-result.json",
    analysis = directory .. "/analysis-result.json",
  }
end

--- A new job record for a started upload; preparation runs on a later tick.
function job.new(job_id, exchange, requested, requested_order)
  local paths = phase_paths(job_id)
  local player = capture.find_player()
  return {
    job_id = job_id,
    blueprint = exchange,
    requested = requested,
    requested_order = requested_order,
    image_path = paths.image,
    preview_result_path = paths.preview,
    analysis_result_path = paths.analysis,
    player_index = player and player.index,
    analysis_done = not requested.analysis,
    preview_done = not requested.preview,
    phase = "preparing",
    prepare_tick = game.tick + 1,
  }
end

--- The start reply, also kept as storage.completed_job once the job is done.
function job.accepted_payload(active)
  local paths = {}
  if active.requested.preview then
    paths.preview_image = active.image_path
    paths.preview_result = active.preview_result_path
  end
  if active.requested.analysis then
    paths.analysis_result = active.analysis_result_path
  end

  local payload = {
    status = "accepted",
    job_id = active.job_id,
    requested_phases = active.requested_order,
    paths = paths,
  }
  if active.preview_ready then
    payload.render = {
      resolution = active.frame.resolution,
      zoom = active.frame.zoom,
      bounding_box = active.bounding_box,
      space_platform = active.space_platform,
    }
  end
  return payload
end

local function phase_state(active, phase)
  if not active.requested[phase] then
    return "not_requested"
  end
  return active[phase .. "_done"] and "finished" or "pending"
end

--- The status command's active_job entry.
function job.status(active)
  return {
    job_id = active.job_id,
    phase = active.phase,
    requested_phases = active.requested_order,
    render = job.accepted_payload(active).render,
    phases = {
      preview = phase_state(active, "preview"),
      analysis = phase_state(active, "analysis"),
    },
  }
end

local function fail_requested_phases(active, code, message)
  if active.requested.analysis then
    phase_result.write_analysis(active, phase_result.failure(active.job_id, "analysis", code, message))
    active.analysis_done = true
  end
  if active.requested.preview then
    phase_result.write_preview(active, phase_result.failure(active.job_id, "preview", code, message))
    active.preview_done = true
  end
end

local function finish_if_complete(active)
  if active.analysis_done and active.preview_done then
    storage.completed_job = job.accepted_payload(active)
    progress.log(active.job_id, "job complete, clearing active job")
    storage.job = nil
    -- Both results are written, so nothing needs the surface any more; a
    -- space platform would otherwise stay until the next job.
    pcall(preview_surface.drop)
  end
end

--- Decodes and materializes the blueprint on a fresh surface. A step that
--- fails writes failure markers for every requested phase.
local function prepare(active)
  progress.log(active.job_id, "prepare_job: starting (requested_phases=" ..
    table.concat(active.requested_order, ",") .. ")")
  local needs_space_platform, platform_error = blueprint.needs_space_platform(active.blueprint)
  if platform_error then
    progress.log(active.job_id, "prepare_job: invalid blueprint - " .. tostring(platform_error))
    fail_requested_phases(active, "invalid_blueprint", platform_error)
    return
  end
  active.space_platform = needs_space_platform
  progress.log(active.job_id, "prepare_job: blueprint decoded (space_platform=" ..
    tostring(needs_space_platform) .. ")")

  local force_ok, force_or_error = pcall(analysis_force.prepare)
  if not force_ok then
    progress.log(active.job_id, "prepare_job: analysis force unavailable - " .. tostring(force_or_error))
    fail_requested_phases(active, "analysis_force_unavailable", tostring(force_or_error))
    return
  end
  active.force = force_or_error
  progress.log(active.job_id, "prepare_job: analysis force ready")

  local surface_ok, surface, surface_error = pcall(preview_surface.create, active.force, needs_space_platform)
  if not surface_ok then
    surface_error = surface
    surface = nil
  end
  if not surface then
    progress.log(active.job_id, "prepare_job: surface unavailable - " .. tostring(surface_error))
    fail_requested_phases(active, "surface_unavailable", tostring(surface_error))
    return
  end
  active.surface_name = surface.name
  progress.log(active.job_id, "prepare_job: surface '" .. surface.name .. "' created")

  local render_ok, placed, render_error = pcall(
    blueprint.render, surface, active.force, active.blueprint)
  if not render_ok then
    render_error = placed
    placed = nil
  end
  if not placed then
    progress.log(active.job_id, "prepare_job: blueprint materialization failed - " .. tostring(render_error))
    fail_requested_phases(active, "blueprint_materialization_failed", tostring(render_error))
    return
  end

  local box, entities = placed.box, placed.entities
  active.bounding_box = box
  active.entities = entities
  active.materialization_issues = placed.issues
  active.footprint = placed.footprint
  active.build_items = placed.build_items
  active.analysis_tick = game.tick + 1
  active.capture_tick = game.tick + math.max(config.render_delay_ticks, 1)
  active.phase = "stabilizing"
  progress.log(active.job_id, "prepare_job: blueprint materialized (" ..
    tostring(entities and #entities or 0) .. " entities), analysis_tick=" ..
    tostring(active.analysis_tick) .. " capture_tick=" .. tostring(active.capture_tick))

  if active.requested.preview then
    preview.prepare(active, surface, box)
  end
  progress.log(active.job_id, "prepare_job: finished, phase=" .. tostring(active.phase))
end

local function run_analysis(active)
  active.phase = "analyzing"
  progress.log(active.job_id, "run_analysis: starting calculation")
  local calculated, result = pcall(analysis.calculate, {
    job_id = active.job_id,
    tick = game.tick,
    entities = active.entities,
    force = active.force,
    surface = game.surfaces[active.surface_name],
    materialization_issues = active.materialization_issues,
    footprint = active.footprint,
    build_items = active.build_items,
  })
  if calculated then
    if phase_result.write_analysis(active, result) then
      progress.log(active.job_id, "run_analysis: succeeded, result written to " .. active.analysis_result_path)
    end
  else
    phase_result.write_analysis(active, phase_result.failure(
      active.job_id, "analysis", "analysis_failed", tostring(result)))
    progress.log(active.job_id, "run_analysis: failed - " .. tostring(result))
  end
  active.analysis_done = true
  active.entities = nil
  active.materialization_issues = nil
  active.build_items = nil
end

--- Advances the active job by one tick.
function job.tick(active)
  if active.phase == "preparing" then
    if game.tick < active.prepare_tick then
      return
    end
    active.phase = "materializing"
    local prepared, prepare_error = pcall(prepare, active)
    active.blueprint = nil
    if not prepared then
      fail_requested_phases(active, "preparation_failed", tostring(prepare_error))
    end
    finish_if_complete(active)
    return
  end

  if active.requested.analysis and not active.analysis_done and game.tick >= active.analysis_tick then
    run_analysis(active)
  end
  if active.requested.preview and not active.preview_done then
    preview.run(active)
  end
  finish_if_complete(active)
end

return job
