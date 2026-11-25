--- RCON command plumbing: JSON payload parsing, phase selection parsing and
--- replies. Reply shapes are part of the bridge protocol (bridge-protocol.md).
local config = require("scripts.config")

local command = {}

--- Replies to whoever issued the command: RCON, or a player typing it.
function command.reply(issued, payload)
  local encoded = helpers.table_to_json(payload)
  if issued.player_index then
    local player = game.get_player(issued.player_index)
    if player then
      player.print(encoded)
    end
    return
  end
  rcon.print(encoded)
end

--- Replies with an error. job_id may be nil (the payload did not parse).
function command.fail(issued, job_id, code, message)
  command.reply(issued, {
    status = "error",
    job_id = job_id,
    error_code = code,
    error = message,
  })
end

--- Decodes a command's JSON payload, which must carry a valid job_id.
function command.parse_payload(parameter)
  if not parameter or parameter == "" then
    return nil, "missing JSON payload"
  end
  local decoded, request = pcall(helpers.json_to_table, parameter)
  if not decoded or type(request) ~= "table" then
    return nil, "payload is not valid JSON"
  end
  if type(request.job_id) ~= "string" or not string.match(request.job_id, config.job_id_pattern) then
    return nil, "job_id must match [A-Za-z0-9][A-Za-z0-9_-]*"
  end
  return request
end

--- Parses a phases array into a set and its order. nil means both phases.
function command.parse_phases(value)
  if value == nil then
    return { preview = true, analysis = true }, { "preview", "analysis" }
  end
  if type(value) ~= "table" then
    return nil, nil, "phases must be an array containing preview and/or analysis"
  end

  local selected = {}
  local ordered = {}
  for index, phase in ipairs(value) do
    if (phase ~= "preview" and phase ~= "analysis") or selected[phase] then
      return nil, nil, "phases contains an unknown or duplicate phase"
    end
    selected[phase] = true
    ordered[index] = phase
  end
  if #ordered == 0 then
    return nil, nil, "phases must not be empty"
  end
  return selected, ordered
end

return command
