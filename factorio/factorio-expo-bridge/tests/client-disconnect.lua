-- A capturing client that leaves mid-preview: the screenshot and its success
-- marker are written by that client, so the server writes the failure marker
-- instead of leaving the runner to wait out the job.
package.loaded["util"] = {}
script = { active_mods = { base = "2.1.24" } }
package.path = arg[1] .. "/?.lua;" .. package.path

local writes
helpers = {
  table_to_json = function(values) return '["' .. values[1] .. '"]' end,
  write_file = function(path, data, append, for_player)
    table.insert(writes, { path = path, data = data, for_player = for_player })
  end,
  take_screenshot = function() end,
}
log = function() end

local connected
game = {
  tick = 100,
  get_player = function(index) return { index = index, connected = connected } end,
  take_screenshot = function() end,
}

local preview = require("scripts.preview-schedule")

local function flushing_job()
  return {
    job_id = "pj_flush", preview_result_path = "previews/pj_flush/preview-result.json",
    image_path = "previews/pj_flush/preview.png", player_index = 1, captured = true,
    capture_tick = 10, finish_tick = 200, frame = { resolution = { x = 1, y = 1 }, zoom = 1 },
    phase = "flushing", preview_done = false,
  }
end

-- Still connected while flushing: nothing is written until the finish tick,
-- then the success marker goes through the client.
writes, connected = {}, true
local job = flushing_job()
preview.run(job)
assert(#writes == 0 and not job.preview_done, "flushing wrote a result too early")
game.tick = 200
preview.run(job)
assert(job.preview_done and #writes == 1, "success marker missing")
assert(writes[1].for_player == 1 and writes[1].data:find('"status":"succeeded"', 1, true), writes[1].data)

-- Disconnected while flushing: the server fails the phase at once.
writes, connected, game.tick = {}, false, 150
job = flushing_job()
preview.run(job)
assert(job.preview_done and #writes == 1, "no failure marker for a client lost while flushing")
assert(writes[1].for_player == 0, "the failure marker must be server-only")
assert(writes[1].data:find('"code":"factorio_client_unavailable"', 1, true), writes[1].data)

-- Disconnected before the capture: the same failure, nothing captured.
writes, connected = {}, false
job = flushing_job()
job.captured, job.finish_tick = false, nil
preview.run(job)
assert(job.preview_done and not job.captured and #writes == 1)
assert(writes[1].for_player == 0 and writes[1].data:find('"code":"factorio_client_unavailable"', 1, true))

-- The client heartbeat: written through the connected client once per interval,
-- never when no client is connected.
local capture = require("scripts.capture")
game.connected_players = { { index = 7, name = "client" } }
writes, game.tick = {}, 120
capture.write_heartbeat()
assert(#writes == 1 and writes[1].for_player == 7 and writes[1].path == "previews/client-heartbeat", "heartbeat not written by the client")
writes, game.tick = {}, 121
capture.write_heartbeat()
assert(#writes == 0, "heartbeat written between intervals")
writes, game.tick, game.connected_players = {}, 180, {}
capture.write_heartbeat()
assert(#writes == 0, "heartbeat written without a client")

print("client disconnect tests passed")
