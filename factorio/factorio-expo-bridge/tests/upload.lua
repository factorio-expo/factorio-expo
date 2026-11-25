package.path = arg[1] .. "/?.lua;" .. package.path

local rendered_blueprint
local results = {}
package.loaded["scripts.analysis"] = {
  calculate = function() return { status = "succeeded" } end,
  write_result = function(path, result) results[path] = result end,
}
package.loaded["scripts.analysis-force"] = { prepare = function() return {} end }
package.loaded["scripts.blueprint"] = {
  needs_space_platform = function() return false end,
  render = function(_, _, exchange)
    assert(storage.job and storage.job.phase == "materializing", "preparation ran before registration")
    rendered_blueprint = exchange
    return {}, {}, {}
  end,
}
package.loaded["scripts.surface"] = {
  create = function() return { name = "test" } end,
  generate_capture_area = function() end,
}
package.loaded["scripts.capture"] = {
  find_player = function()
    return { index = 1, name = "test", force = { chart = function() end } }
  end,
  frame = function() return { resolution = { x = 100, y = 50 }, zoom = 1 } end,
  chart_area = function() return {} end,
  take = function() end,
  write_heartbeat = function() end,
  write_result = function(job, result) results[job.preview_result_path] = result end,
}

local response
local logs
helpers = {
  json_to_table = function(request) return request end,
  table_to_json = function(payload) return payload end,
}
rcon = { print = function(payload) response = payload end }
log = function(message) table.insert(logs, message) end

local bridge = require("scripts.bridge")
local config = require("scripts.config")

local function reset()
  storage = {}
  game = { tick = 0, connected_players = { 1 } }
  logs = {}
  rendered_blueprint = nil
  results = {}
  bridge.on_init()
end

local function command(handler, request, status, code)
  response = nil
  handler({ parameter = request })
  assert(response and response.status == status, "unexpected response status")
  assert(response.job_id == request.job_id, "response job_id mismatch")
  assert(response.error_code == code, "unexpected error_code")
  return response
end

local function begin(count)
  command(bridge.on_upload_begin, { job_id = "pj_test", chunk_count = count }, "accepted")
end

local function chunk(index)
  return { job_id = "pj_test", index = index, data = string.rep("a", 800) }
end

-- Match the failed 297,933-byte upload, with progress spanning more than 30s.
reset()
begin(373)
local chunks = {}
for index = 1, 373 do
  game.tick = game.tick + 6
  bridge.on_tick()
  local request = chunk(index)
  if index == 373 then request.data = string.rep("a", 333) end
  chunks[index] = request.data
  command(bridge.on_upload_chunk, request, "accepted")
end
assert(game.tick > config.upload_timeout_ticks)
local accepted = command(bridge.on_upload_start, { job_id = "pj_test" }, "accepted")
assert(rendered_blueprint == nil, "start ACK performed expensive preparation")
assert(storage.upload == nil and storage.job.job_id == "pj_test")
assert(accepted.requested_phases[1] == "preview" and accepted.requested_phases[2] == "analysis")
assert(#accepted.requested_phases == 2)
assert(accepted.paths.preview_image == "previews/pj_test/preview.png")
assert(accepted.paths.preview_result == "previews/pj_test/preview-result.json")
assert(accepted.paths.analysis_result == "previews/pj_test/analysis-result.json")
assert(accepted.render == nil, "fast ACK must not claim a render profile")
assert(storage.job.phase == "preparing")
bridge.on_tick()
assert(rendered_blueprint == nil, "preparation ran in the command's tick")
game.tick = game.tick + 1
bridge.on_tick()
assert(rendered_blueprint == table.concat(chunks), "upload contents changed")
assert(storage.job.blueprint == nil, "prepared payload was retained")
bridge.on_status_command({})
assert(response.protocol_version == config.protocol_version, "status must report the protocol version")
assert(response.active_job.render.resolution.x == 100)
game.surfaces = { test = {} }
game.get_player = function() return { connected = true } end
game.tick = game.tick + config.render_delay_ticks + 1
bridge.on_tick()
game.tick = game.tick + config.flush_delay_base_ticks + config.flush_delay_ticks_per_megapixel
bridge.on_tick()
assert(storage.job == nil, "complete phases did not clear active job")
bridge.on_status_command({})
assert(response.completed_job.render.resolution.x == 100)
assert(results[accepted.paths.analysis_result].status == "succeeded")
assert(results[accepted.paths.preview_result].status == "succeeded")

-- Analysis-only jobs also prepare asynchronously without a capture player.
reset()
game.connected_players = {}
game.surfaces = { test = {} }
command(bridge.on_upload_begin, { job_id = "analysis_only", chunk_count = 1, phases = { "analysis" } }, "accepted")
command(bridge.on_upload_chunk, { job_id = "analysis_only", index = 1, data = "0a" }, "accepted")
command(bridge.on_upload_start, { job_id = "analysis_only" }, "accepted")
assert(rendered_blueprint == nil)
game.tick = game.tick + 1
bridge.on_tick()
assert(storage.job ~= nil, "analysis skipped its stabilization tick")
game.tick = game.tick + 1
bridge.on_tick()
assert(storage.job == nil)
assert(results["previews/analysis_only/analysis-result.json"].status == "succeeded")

-- Abandoned uploads expire even when no new command arrives.
reset()
begin(2)
game.tick = config.upload_timeout_ticks
bridge.on_tick()
assert(storage.upload ~= nil, "expired at the inclusive timeout boundary")
game.tick = game.tick + 1
bridge.on_tick()
assert(storage.upload == nil and #logs == 1)
command(bridge.on_upload_chunk, chunk(1), "error", "upload_not_found")
begin(2)

-- Only valid chunks refresh the idle timer, and they do not reset total age.
reset()
begin(2)
game.tick = config.upload_timeout_ticks - 1
command(bridge.on_upload_chunk, chunk(1), "accepted")
assert(storage.upload.started_tick == 0)
local last_tick = storage.upload.last_chunk_tick
game.tick = game.tick + 1
command(bridge.on_upload_chunk, chunk(1), "error", "invalid_chunk")
assert(storage.upload.last_chunk_tick == last_tick)
game.tick = last_tick + config.upload_timeout_ticks + 1
bridge.on_tick()
assert(storage.upload == nil)

reset()
begin(4096)
local index = 1
while game.tick + config.upload_timeout_ticks - 1 <= config.upload_max_duration_ticks do
  game.tick = game.tick + config.upload_timeout_ticks - 1
  command(bridge.on_upload_chunk, chunk(index), "accepted")
  index = index + 1
end
game.tick = config.upload_max_duration_ticks + 1
bridge.on_tick()
assert(storage.upload == nil and #logs == 1, "total lifetime was not bounded")

-- Persisted uploads from before the idle timestamp existed still expire safely.
reset()
storage.upload = { job_id = "pj_test", started_tick = 0 }
game.tick = config.upload_timeout_ticks + 1
bridge.on_tick()
assert(storage.upload == nil)

print("upload timeout and response contract tests passed")
