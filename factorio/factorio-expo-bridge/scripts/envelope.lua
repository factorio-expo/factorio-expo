--- The one builder for result-file envelopes. Every phase result (analysis,
--- preview, failure) carries the same five transport fields; the Go side
--- validates them in factorio-expo/analysisresult and the runner's marker code.
local envelope = {}

envelope.schema_version = 1

local function build(status, phase, job_id, tick, fields)
  local result = {}
  for key, value in pairs(fields or {}) do
    result[key] = value
  end
  result.schema_version = envelope.schema_version
  result.status = status
  result.phase = phase
  result.job_id = job_id
  result.tick = tick
  return result
end

function envelope.success(phase, job_id, tick, fields)
  return build("succeeded", phase, job_id, tick, fields)
end

function envelope.failure(phase, job_id, tick, code, message)
  return build("failed", phase, job_id, tick, { error = { code = code, message = message } })
end

--- A failure marker as a hand-built string, for when encoding or writing the
--- real result threw. It uses only string.format, so it cannot throw itself;
--- the job id is reduced to the characters the runner generates.
function envelope.failure_json(phase, job_id, tick, code, message)
  local safe_id = string.gsub(tostring(job_id), "[^%w_%-]", "")
  return string.format(
    '{"error":{"code":"%s","message":"%s"},"job_id":"%s","phase":"%s","schema_version":%d,"status":"failed","tick":%d}',
    code, message, safe_id, phase, envelope.schema_version, math.floor(tonumber(tick) or 0))
end

return envelope
