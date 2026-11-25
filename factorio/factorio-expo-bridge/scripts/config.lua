-- Shared tuning values for the preview pipeline. Keeping them in one module
-- makes the render contract (surface, framing, output layout) explicit.
return {
  -- Go<->Lua contract version, reported by the status command. Keep equal to
  -- bridge.ProtocolVersion in apps/expo-render-worker/internal/bridge/protocol.go.
  protocol_version = 1,
  status_command_name = "bp-preview-status",
  upload_begin_command_name = "bp-preview-begin",
  upload_chunk_command_name = "bp-preview-chunk",
  upload_start_command_name = "bp-preview-start",
  surface_prefix = "bp-preview-",
  -- The origin: platforms are built with absolute snapping, which keeps their
  -- blueprint coordinates only inside the origin grid cell (blueprint.lua).
  build_position = { x = 0, y = 0 },
  output_dir = "previews",
  -- File the capturing client rewrites every heartbeat_interval_ticks; the runner
  -- treats a stale one as a dead client. Keep equal to bridge.ClientHeartbeatFile.
  client_heartbeat_file = "client-heartbeat",
  heartbeat_interval_ticks = 60,

  -- Extra tiles kept around the blueprint so nothing touches the image border.
  margin_tiles = 2,
  -- Extra chunks generated around the estimated footprint before building.
  chunk_padding = 2,
  max_generated_chunks = 4096,

  -- Factorio renders one tile as 32 pixels at zoom 1.
  pixels_per_tile = 32,
  max_resolution = 4096,
  max_anti_alias_resolution = 4096,
  min_zoom = 0.03125,
  max_zoom = 1.0,

  -- Ticks to wait after building so the factorio client has received and
  -- rendered the new surface before the screenshot request is queued.
  render_delay_ticks = 60,
  -- Screenshot encoding is asynchronous and scales with pixel count. Delay the
  -- completion marker long enough for the client to flush larger images.
  flush_delay_base_ticks = 60,
  flush_delay_ticks_per_megapixel = 15,

  -- Large uploads use hundreds of sequential RCON commands. Expire inactive
  -- transfers, but also bound their total lifetime so later jobs cannot starve.
  upload_timeout_ticks = 60 * 30,
  upload_max_duration_ticks = 60 * 60 * 10,
  -- Upload limits; keep equal to maxUploadChunk and chunkSize in
  -- apps/expo-render-worker/internal/bridge/protocol.go (a Go test pins them).
  upload_max_chunks = 4096,
  upload_chunk_length = 800,
  -- Job ids the runner generates: [A-Za-z0-9][A-Za-z0-9_-]*.
  job_id_pattern = "^[%w][%w%-_]*$",
}
