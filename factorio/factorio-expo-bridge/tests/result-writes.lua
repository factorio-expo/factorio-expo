-- Failure paths of the result files: who writes them (for_player), and that an
-- encode or write error still leaves a marker the runner can read.
package.loaded["util"] = {}
script = { active_mods = { base = "2.1.24" } }
package.path = arg[1] .. "/?.lua;" .. package.path

local writes
helpers = {
  table_to_json = function(values) return '["' .. values[1] .. '"]' end,
  write_file = function(path, data, append, for_player)
    table.insert(writes, { path = path, data = data, append = append, for_player = for_player })
  end,
}
log = function() end

local analysis = require("scripts.analysis")
local capture = require("scripts.capture")
local envelope = require("scripts.envelope")
local json = require("scripts.json")

local function reset() writes = {} end

-- The analysis result is written by the server only (player index 0).
reset()
analysis.write_result("previews/j/analysis-result.json", envelope.success("analysis", "j", 1, {}))
assert(#writes == 1 and writes[1].for_player == 0, "analysis result must be server-only")

-- A preview success marker is written for the client that took the screenshot.
reset()
local job = { preview_result_path = "previews/j/preview-result.json", player_index = 3 }
capture.write_result(job, envelope.success("preview", "j", 1, {}))
assert(writes[1].for_player == 3, "success marker must use the client index")

-- A preview failure marker is written by the server: the client may be gone.
reset()
capture.write_result(job, envelope.failure("preview", "j", 1, "factorio_client_unavailable", "gone"))
assert(writes[1].for_player == 0, "failure marker must not go through the client")
job.player_index = nil
capture.write_result(job, envelope.failure("preview", "j", 1, "factorio_client_unavailable", "none"))
assert(writes[1].for_player == 0 and writes[2].for_player == 0)

-- The hand-built fallback marker is valid, complete and cannot throw.
local marker = envelope.failure_json("analysis", 'pj_1"\\y', 42.9, "analysis_failed", "boom")
assert(marker:find('"job_id":"pj_1y"', 1, true), marker)
assert(marker:find('"status":"failed"', 1, true) and marker:find('"phase":"analysis"', 1, true))
assert(marker:find('"schema_version":1', 1, true) and marker:find('"tick":42', 1, true))
assert(marker:find('"code":"analysis_failed"', 1, true))
assert(envelope.failure_json("preview", nil, nil, "preview_failed", "x"):find('"tick":0', 1, true))

-- The bridge writes the fallback marker when encoding or writing throws.
package.loaded["scripts.analysis"] = {
  calculate = function() return { status = "succeeded", rate = math.huge } end,
  write_result = analysis.write_result,
}
package.loaded["scripts.blueprint"] = {}
package.loaded["scripts.surface"] = {}
local real_capture_write = capture.write_result
local bridge_capture = setmetatable({
  write_result = function(...) return real_capture_write(...) end,
}, { __index = capture })
package.loaded["scripts.capture"] = bridge_capture
game = {
  tick = 7,
  surfaces = { test = {} },
  get_player = function(index) return { index = index, connected = true } end,
}
storage = {}
rcon = { print = function() end }
local bridge = require("scripts.bridge")

-- non-finite number: json.encode throws, so the analysis marker must be the fallback.
reset()
storage.job = {
  job_id = "pj_nan", requested = { analysis = true }, requested_order = { "analysis" },
  analysis_result_path = "previews/pj_nan/analysis-result.json",
  surface_name = "test", entities = {}, analysis_tick = 0, analysis_done = false,
  preview_done = true, phase = "stabilizing",
}
bridge.on_tick()
assert(#writes == 1, "expected exactly the fallback marker")
assert(writes[1].for_player == 0, "fallback marker must be server-only")
assert(writes[1].data:find('"code":"analysis_failed"', 1, true), writes[1].data)
assert(writes[1].data:find('"job_id":"pj_nan"', 1, true))

-- a write that throws still falls back to a marker (preview side).
reset()
local failures = 0
helpers.write_file = function(path, data, append, for_player)
  if data:find("succeeded", 1, true) then
    failures = failures + 1
    error("disk full")
  end
  table.insert(writes, { path = path, data = data, for_player = for_player })
end
storage.job = {
  job_id = "pj_disk", requested = { preview = true }, requested_order = { "preview" },
  preview_result_path = "previews/pj_disk/preview-result.json",
  image_path = "previews/pj_disk/preview.png", player_index = 2, captured = true,
  finish_tick = 0, frame = { resolution = { x = 1, y = 1 }, zoom = 1 },
  analysis_done = true, preview_done = false, phase = "flushing",
}
bridge.on_tick()
assert(failures == 1 and #writes == 1, "fallback marker missing after a failed write")
assert(writes[1].data:find('"code":"preview_failed"', 1, true) and writes[1].for_player == 0)

print("result write tests passed")
