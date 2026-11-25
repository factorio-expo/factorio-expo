--- Writes the per-phase result files the runner polls for. When the real
--- result cannot be encoded or written, a hand-built failure marker takes its
--- place so the runner sees an outcome instead of waiting out the job.
local analysis = require("scripts.analysis")
local capture = require("scripts.capture")
local envelope = require("scripts.envelope")

local phase_result = {}

function phase_result.failure(job_id, phase, code, message)
  return envelope.failure(phase, job_id, game.tick, code, message)
end

local function write_fallback_marker(path, phase, job_id, code, write_error)
  log("Factorio Expo could not write " .. phase .. " result: " .. tostring(write_error))
  local marker = envelope.failure_json(phase, job_id, game.tick, code,
    "the " .. phase .. " result could not be encoded or written")
  local written, fallback_error = pcall(helpers.write_file, path, marker, false, 0)
  if not written then
    log("Factorio Expo could not write " .. phase .. " failure marker: " .. tostring(fallback_error))
  end
end

--- Returns whether the real result (not the fallback marker) was written.
function phase_result.write_analysis(job, result)
  local written, write_error = pcall(analysis.write_result, job.analysis_result_path, result)
  if not written then
    write_fallback_marker(job.analysis_result_path, "analysis", job.job_id, "analysis_failed", write_error)
  end
  return written
end

--- Returns whether the real result (not the fallback marker) was written.
function phase_result.write_preview(job, result)
  local written, write_error = pcall(capture.write_result, job, result)
  if not written then
    write_fallback_marker(job.preview_result_path, "preview", job.job_id, "preview_failed", write_error)
  end
  return written
end

return phase_result
