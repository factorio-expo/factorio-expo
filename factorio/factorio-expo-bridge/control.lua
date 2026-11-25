local config = require("scripts.config")
local bridge = require("scripts.bridge")
local initialization = require("scripts.initialization")

local function on_init()
  bridge.on_init()
  initialization.on_init()
end

local function on_configuration_changed()
  bridge.on_configuration_changed()
  initialization.on_configuration_changed()
end

script.on_init(on_init)
script.on_configuration_changed(on_configuration_changed)
script.on_event(defines.events.on_player_created, initialization.on_player_ready)
script.on_event(defines.events.on_cutscene_cancelled, initialization.on_player_ready)
script.on_event(defines.events.on_tick, bridge.on_tick)

-- Registered at load time rather than inside on_init so the command also exists
-- after the save is loaded again; on_init only runs once per save.
commands.add_command(config.status_command_name, "Reports bridge readiness.", bridge.on_status_command)
commands.add_command(config.upload_begin_command_name, "Begins a chunked bridge upload.", bridge.on_upload_begin)
commands.add_command(config.upload_chunk_command_name, "Adds a chunk to a bridge upload.", bridge.on_upload_chunk)
commands.add_command(config.upload_start_command_name, "Starts a completed bridge upload.", bridge.on_upload_start)
