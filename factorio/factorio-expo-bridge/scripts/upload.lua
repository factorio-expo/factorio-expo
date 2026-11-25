--- The chunked upload buffer (storage.upload): one transfer at a time, bounded
--- in chunk count and chunk length, expiring when idle or too old. Errors are
--- returned as (code, message) for the command handler to reply with.
local config = require("scripts.config")

local upload = {}

--- Drops an upload that went idle too long or exceeded its total lifetime.
function upload.clear_expired()
  local current = storage.upload
  if not current then
    return
  end
  if not current.started_tick then
    log("Factorio Expo cleared legacy upload for job " .. tostring(current.job_id))
    storage.upload = nil
    return
  end
  local last_chunk_tick = current.last_chunk_tick or current.started_tick
  if game.tick - last_chunk_tick <= config.upload_timeout_ticks
    and game.tick - current.started_tick <= config.upload_max_duration_ticks then
    return
  end
  log("Factorio Expo cleared expired upload for job " .. tostring(current.job_id))
  storage.upload = nil
end

function upload.valid_chunk_count(count)
  return type(count) == "number"
    and count >= 1
    and count <= config.upload_max_chunks
    and count == math.floor(count)
end

--- Starts buffering an upload. phases is { selected = set, ordered = list }.
function upload.begin(job_id, chunk_count, phases)
  storage.upload = {
    job_id = job_id,
    chunk_count = chunk_count,
    chunks = {},
    phases = phases,
    started_tick = game.tick,
    last_chunk_tick = game.tick,
  }
end

--- Appends the next chunk. Only a valid, in-order chunk refreshes the idle timer.
function upload.add_chunk(request)
  local current = storage.upload
  if not current or current.job_id ~= request.job_id then
    return "upload_not_found", "matching upload was not found"
  end
  if request.index ~= #current.chunks + 1
    or type(request.data) ~= "string"
    or #request.data < 1
    or #request.data > config.upload_chunk_length then
    return "invalid_chunk", "upload chunk is invalid or out of order"
  end
  table.insert(current.chunks, request.data)
  current.last_chunk_tick = game.tick
end

--- Removes a complete upload from the buffer and returns
--- { job_id, blueprint, phases }, or nil, code, message.
function upload.take(job_id)
  local current = storage.upload
  if not current or current.job_id ~= job_id or #current.chunks ~= current.chunk_count then
    return nil, "upload_incomplete", "upload is incomplete"
  end
  storage.upload = nil
  return {
    job_id = current.job_id,
    blueprint = table.concat(current.chunks),
    phases = current.phases,
  }
end

return upload
