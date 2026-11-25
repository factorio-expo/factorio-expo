--- The bridge's command handlers and tick entry point (registered in
--- control.lua). Payload parsing and replies live in command.lua, the upload
--- buffer in upload.lua, the job state machine in job.lua and capture
--- scheduling in preview-schedule.lua.
local capture = require("scripts.capture")
local command = require("scripts.command")
local config = require("scripts.config")
local job = require("scripts.job")
local progress = require("scripts.progress")
local upload = require("scripts.upload")

local bridge = {}

--- Registers a job for a completed upload and acknowledges at once; the
--- expensive preparation runs on a later tick.
local function start_job(issued, request, inherited_phases)
  progress.log(request.job_id, "start_job: received request")
  if storage.job or storage.upload then
    local active_job_id = storage.job and storage.job.job_id or storage.upload.job_id
    progress.log(request.job_id, "start_job: bridge busy with '" .. active_job_id .. "'")
    return command.fail(issued, request.job_id, "bridge_busy",
      "job or upload '" .. active_job_id .. "' is still active")
  end

  local requested, requested_order, phases_error
  if request.phases ~= nil then
    requested, requested_order, phases_error = command.parse_phases(request.phases)
  elseif inherited_phases then
    requested = inherited_phases.selected
    requested_order = inherited_phases.ordered
  else
    requested, requested_order = command.parse_phases(nil)
  end
  if not requested then
    return command.fail(issued, request.job_id, "invalid_request", phases_error)
  end

  local active = job.new(request.job_id, request.blueprint, requested, requested_order)
  storage.completed_job = nil
  storage.job = active
  command.reply(issued, job.accepted_payload(active))
end

local function reset()
  storage.job = nil
  storage.upload = nil
  storage.completed_job = nil
end

function bridge.on_init()
  reset()
end

function bridge.on_configuration_changed()
  reset()
end

function bridge.on_status_command(issued)
  upload.clear_expired()

  local active_job
  if storage.job then
    active_job = job.status(storage.job)
  elseif storage.upload then
    active_job = {
      job_id = storage.upload.job_id,
      phase = "uploading",
      requested_phases = storage.upload.phases.ordered,
    }
  end

  local player = capture.find_player()
  command.reply(issued, {
    status = "ok",
    protocol_version = config.protocol_version,
    capture = {
      connected = player ~= nil,
      player = player and player.name or nil,
    },
    active_job = active_job,
    completed_job = storage.completed_job,
  })
end

function bridge.on_upload_begin(issued)
  local request, parse_error = command.parse_payload(issued.parameter)
  if not request then
    return command.fail(issued, nil, "invalid_request", parse_error)
  end
  if storage.job or storage.upload then
    return command.fail(issued, request.job_id, "bridge_busy", "another upload or job is still active")
  end
  if not upload.valid_chunk_count(request.chunk_count) then
    return command.fail(issued, request.job_id, "invalid_request", "chunk_count is invalid")
  end
  local selected, ordered, phases_error = command.parse_phases(request.phases)
  if not selected then
    return command.fail(issued, request.job_id, "invalid_request", phases_error)
  end

  upload.begin(request.job_id, request.chunk_count, { selected = selected, ordered = ordered })
  command.reply(issued, {
    status = "accepted",
    job_id = request.job_id,
    requested_phases = ordered,
  })
end

function bridge.on_upload_chunk(issued)
  upload.clear_expired()

  local request, parse_error = command.parse_payload(issued.parameter)
  if not request then
    return command.fail(issued, nil, "invalid_request", parse_error)
  end
  local code, message = upload.add_chunk(request)
  if code then
    return command.fail(issued, request.job_id, code, message)
  end
  command.reply(issued, { status = "accepted", job_id = request.job_id })
end

function bridge.on_upload_start(issued)
  upload.clear_expired()

  local request, parse_error = command.parse_payload(issued.parameter)
  if not request then
    return command.fail(issued, nil, "invalid_request", parse_error)
  end
  local completed, code, message = upload.take(request.job_id)
  if not completed then
    return command.fail(issued, request.job_id, code, message)
  end
  return start_job(issued, {
    job_id = completed.job_id,
    blueprint = completed.blueprint,
    phases = request.phases,
  }, completed.phases)
end

function bridge.on_tick()
  capture.write_heartbeat()
  upload.clear_expired()
  if storage.job then
    job.tick(storage.job)
  end
end

return bridge
