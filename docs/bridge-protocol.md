# Bridge protocol (Go runner <-> Lua bridge mod)

The runner and the bridge are one versioned contract. Changing a field on either
side without the other stalls or fails jobs. Use this file as the checklist.

| Side | Code |
|---|---|
| Go, RCON commands + replies | `apps/expo-render-worker/internal/bridge/client.go` (+ `rcon/client.go`) |
| Go, result files on the inbox | `apps/expo-render-worker/internal/artifacts/inbox.go` (read + validate; `store.go` keeps the retained copies) |
| Go, orchestration + error mapping | `apps/expo-render-worker/internal/worker/worker.go`, `worker/phase.go` (`readPhase`/`settlePhase`), `worker/runwatch.go` (status of the running job) |
| Go, analysis payload (runner and API) | `internal/analysisresult/` (`ReadAll`, `Parse`, `ParseSnapshot`) |
| Lua, command handlers | `factorio/factorio-expo-bridge/scripts/bridge.lua` (registered in `control.lua`); payload parsing + replies `scripts/command.lua`; upload buffer `scripts/upload.lua` |
| Lua, job state machine | `scripts/job.lua` (record, accepted/status payloads, tick phases); capture scheduling `scripts/preview-schedule.lua`; result writing + fallback marker `scripts/phase-result.lua` |
| Lua, constants | `scripts/config.lua` |
| Lua, preview result | `scripts/capture.lua` (`write_result`) |
| Lua, analysis result | `scripts/analysis.lua` (`calculate`) |

**Protocol version.** One integer: Lua `config.protocol_version`, Go `bridge.ProtocolVersion` (`bridge/protocol.go`; a Go test pins the two files together). The status reply carries it. `bridge.Client.Status` returns `ErrProtocolMismatch` for any other value, a missing field counts as 0. Readiness reports the bridge as `incompatible`, the worker claims no job while it is, and a run that meets it mid-job stops at once instead of looping to `capture_timeout`. Bump it on any incompatible change to a command, reply or result-file field; adding an optional Lua field does **not** need a bump, but the Go struct must gain the field first (strict decoding). Go owns command names, limits, the id pattern and the inbox layout in `bridge/protocol.go` (`RelativePath`, `PreviewResultFile`, ...); Lua keeps its copies in `config.lua` (and the inbox file names in `job.lua`).

Tests that pin the shapes: Go `bridge/{client,console,protocol,status,upload}_test.go`,
`artifacts/inbox_test.go`, `worker/worker_test.go`, `worker/runwatch_test.go`,
`internal/analysisresult/analysisresult_test.go` (golden files in `test-data/analysis/`); Lua `factorio-expo-bridge/tests/*.lua`
(`upload.lua` covers the commands, `client-disconnect.lua` a capturing client that leaves). Lua tests are plain scripts that take the mod root as `arg[1]`; `go test ./apps/expo-render-worker/internal/bridge/` (`TestLuaUploadTimeoutAndResponseContract`) runs them when `lua` is on PATH and skips otherwise. Update the matching test with every change.

## Two channels

1. **RCON commands** (runner -> server): the runner calls `/command {json}`; the bridge replies with one JSON object on one line via `rcon.print`. Factorio puts the console messages logged since the previous RCON command (e.g. `2026-10-27 09:30:01 [JOIN] ... joined the game`) in front of a command's output, so Go decodes only the **last line** of a response; replies must stay single-line (`helpers.table_to_json` output is). Go decodes replies with `DisallowUnknownFields` and requires exactly one JSON value, so **adding a field to a Lua reply that the Go struct lacks breaks decoding**. Add the Go field first (or in the same change).
2. **Result files** (bridge -> inbox volume; plus `client-heartbeat` directly in the inbox, see below): the bridge writes JSON under `previews/<job_id>/`; the runner polls and validates them. A preview *success* marker is written by the graphical client (`helpers.write_file(..., player_index)`, next to the screenshot). Every other result file is written with `for_player = 0` (server only): the analysis result (every peer runs the script, so an unaddressed write would be repeated by each client) and every preview *failure* marker (the client may be the thing that disconnected). If encoding or writing a result throws, the bridge writes a hand-built failure marker (`analysis_failed` / `preview_failed`, `envelope.failure_json`) so the runner never waits out the job timeout.

Job ids must match `^[%w][%w%-_]*$`. Only one upload or job is active at a time
(`bridge_busy` otherwise).

## RCON commands

| Command | Request JSON | Success reply |
|---|---|---|
| `/bp-preview-status` | none | `{status:"ok", protocol_version, capture:{connected, player?}, active_job?, completed_job?}` |
| `/bp-preview-begin` | `{job_id, chunk_count (1..4096)}` | `{status:"accepted", job_id, requested_phases}` |
| `/bp-preview-chunk` | `{job_id, index (1-based, in order), data (1..800 chars)}` | `{status:"accepted", job_id}` |
| `/bp-preview-start` | `{job_id, phases?: ["preview","analysis"]}` | `{status:"accepted", job_id, requested_phases, paths, render?}` (the "accepted" object) |

Go constants: `chunkSize = 800`, `maxUploadChunk = 4096` (`bridge/protocol.go`); Lua:
`config.upload_chunk_length`, `config.upload_max_chunks`. A Go test pins them equal. Omitted `phases` means both.
The runner sends the same phase list to `start` that it expects back, in order
(`samePhases`); it also checks artifact paths and render sanity.

Replies:

```text
accepted : {status:"accepted", job_id, requested_phases:[...],
            paths:{preview_image?, preview_result?, analysis_result?},
            render?:{resolution:{x,y}, zoom, bounding_box, space_platform}}
            paths are "previews/<job_id>/preview.png|preview-result.json|analysis-result.json"
            render appears once the preview frame is known (not in the start reply
            until preparation finished, so the runner re-reads it from status)
status   : protocol_version (int, = config.protocol_version); active_job:{job_id, phase, requested_phases?, phases:{preview,analysis}, render?}
            phases values: "pending" | "finished" | "not_requested"
            phase: "uploading" | "preparing" | "materializing" | "stabilizing" |
                   "analyzing" | "waiting_for_capture" | "capturing" | "flushing"
           completed_job: the last accepted object, kept until the next job
error    : {status:"error", job_id?, error_code, error}
```

Go types: `Status`, `ActiveJob`, `PhaseStates`, `Accepted`, `AcceptedPaths`,
`Render`, `Resolution`, `actionResponse` (client.go). Lua builders:
`accepted_payload`, `bridge.on_status_command`, inline `reply(...)` calls.

Reply error codes (Lua -> `bridge.Error.Code`): `invalid_request`, `bridge_busy`,
`upload_not_found`, `invalid_chunk`, `upload_incomplete`. The worker fails
the job with `blueprint_too_large` (also raised locally by `bridge.Submit` before any RCON call);
`invalid_blueprint` never arrives in a reply, it is reported through the phase result files; after any other
bridge error the worker reconciles status and the inbox (the RCON reply may have been lost) and fails the job
with `bridge_submission_failed` only if the bridge did not start it.

## Result files

Built in Lua by `scripts/envelope.lua` (the only envelope builder). Both carry `schema_version: 1`, `status: "succeeded"|"failed"`, `phase`, `job_id`
and `tick`. A failed file also has `error:{code, message}` (both non-empty).

`preview-result.json` (Lua `capture.write_result`; Go `Inbox.PreviewMarker`, max 64 KiB, regular file only):
success adds `image_path` (must equal `previews/<job_id>/preview.png`),
`resolution:{x,y}`, `zoom`, `bounding_box`, `space_platform`. The worker compares
resolution, zoom and `space_platform` with the render reported over RCON and fails
the phase with `result_mismatch` if they differ; PNG dimensions/size/structure are
then validated by `Inbox.OpenPreviewImage`, and the retained copy's chunks again by `Store.RetainPreview`.

`analysis-result.json` (Lua `analysis.calculate`; Go: the single parser `analysisresult.Parse`,
shared by the runner `Inbox.Analysis` (reads and parses the file once; `Store.RetainAnalysis` keeps exactly those bytes) and the API `preview.Dispatcher`; max 8 MiB, `analysisresult.MaxBytes`): success adds `state` (`complete|partial`),
`ingredients[]`, `products[]`, `intermediates[]`, `electricity`, `pollution`,
`completeness`, `calculator`, `space_platform`, `footprint`, `build_cost`. There are no warnings or assumptions: an entity the
analysis fails on (a technical cause, not something a blueprint lacks such as a furnace recipe or fuel) counts as unresolved in `completeness`, which makes the result `partial`; `completeness.unresolved[]` lists `{cause, entity, count}` (causes: `unavailable_prototype`, `entity_not_materialized`, `missing_modules`, `calculation_error`, `unknown_material`, `invalid_value`, `fluid_unresolved`). Inside the bridge, each materialization issue carries `cause` and `entity` beside its `affects_*` flags.
The parser ignores keys it does not know.
Field-level rules `analysisresult` enforces (the same parser on the runner and the API; the last two bullets are API-side storage facts, not parser rules):

- Materials: `type` `item|fluid`; items need non-empty `quality`, fluids null `quality`. A material carries no temperature: the bridge keys a fluid by its name alone and sums its rates over every temperature (boilers make steam at their target temperature, generators take it at the filter's default, recipe fluids name none; keyed by temperature, one fluid split into a product and an ingredient). Rates finite and >= 0; ingredients need `input_per_second`, products `output_per_second`, intermediates both plus `difference_per_second` within `1e-9 * max(1, input, output)` of `output - input` (the bridge writes numbers with the shortest round-tripping encoding, `json.lua`).
- `electricity`: finite `consumption_watts` (what the entities use at full load, drain included), `production_watts` (what generators, solar panels and electric energy interfaces make), `drain_watts` (>= 0), `net_watts` (production minus consumption). `Snapshot.Facts()` keeps all three; the search index's per-MW rates divide by `consumption_watts`. `pollution`: finite `production_per_second`, `absorption_per_second` (>= 0), `net_per_second`.
- `schema_version` must equal `analysisresult.SchemaVersion` (1); `job_id` must match the job; `tick` is required.
- `footprint` `{width, height}`: integers >= 1, the tile-aligned extent of everything placed (entities by their collision boxes, tiles by their tile; `blueprint.render`).
- `build_cost` `{items, raw}`: `items` `[{type: "item", name, quality, count}]` (count an integer >= 1) are what placing the blueprint takes: each entity's first `items_to_place_this` at the entity's quality, its item requests (modules, fuel), and each tile's placing item (`scripts/build-cost.lua`); `raw` `[{type: item|fluid, name, amount}]` (finite, > 0, largest first) breaks them down through each material's visible recipe of the same name, net of what it consumes of itself, at normal quality without productivity; a material with no such recipe, or met again inside its own chain, is raw.
- `space_platform` is required and must contain `is_space_platform`; platform leaves need positive `weight_tons` and non-negative `thrust_mn`, `max_speed_km_s`; non-platform results must not carry them.
- The API stores `ingredients, products, intermediates, electricity, pollution, completeness, calculator, space_platform, footprint, build_cost` verbatim as `blueprint_stats.production` (`Result.Stored()`); the API view (`Snapshot.View()`), search facts (`Snapshot.Facts()`) and the web `Production` type (`apps/expo-web/src/lib/api/types.ts`) read from that snapshot via `analysisresult.ParseSnapshot`. A stored snapshot without `space_platform` reads as a non-platform; one without `footprint` or `build_cost` has neither.
- Golden fixtures: `test-data/analysis/*.json`, written by `tests/analysis-golden.lua` with the real encoder, decoded by the Go tests.

## Failure codes and retry classification

Phase failures written by Lua (`phase_result.failure`, `scripts/phase-result.lua`) surface as runner phase failures
with the same `code`: `invalid_blueprint`, `analysis_force_unavailable`,
`surface_unavailable`, `blueprint_materialization_failed`, `preparation_failed`,
`factorio_client_unavailable`, `blueprint_too_large`,
`preview_preparation_failed`, `capture_failed`, `analysis_failed`, `preview_failed` (result could not be encoded/written). Runner-origin
codes: `bridge_submission_failed`, `capture_timeout`, `interrupted` (lease
expired or runner restarted mid-job), `factorio_restarted`, `result_missing`,
`invalid_result`, `result_mismatch`, plus `request_too_large` (HTTP 413) from
the runner HTTP API.

While a job runs the worker reads `bp-preview-status` on every poll
(`worker/runwatch.go`), so a run that can no longer deliver ends at once
instead of at `capture_timeout`:

- `factorio_restarted`: the bridge answers but the job is neither its
  `active_job` nor its `completed_job`, and no result file exists. A server
  restart reloads the save (autosave is off and a SIGTERM does not save), so
  the bridge has forgotten the job. An unanswered status decides nothing. Not
  retryable (`jobs.Retryable`): the worker never restarts Factorio and a
  compose deploy stops the worker first (`depends_on` `restart: true`), so a
  restart during a job almost always means the blueprint crashed the server.
  Running it again would only crash it again.
- `result_missing`: the bridge reports the job completed but a phase result is
  still missing 30 s later. The preview success marker is written by the
  capturing client; a client killed after the capture never writes it, and the
  server still counts it as connected for ~20 s. A client the server already
  saw leave fails the preview from the bridge instead
  (`factorio_client_unavailable`, before the capture or while flushing).

Client heartbeat: on every `heartbeat_interval_ticks` (60) tick the bridge
rewrites `previews/client-heartbeat` through the connected client
(`capture.write_heartbeat`, `helpers.write_file(..., player.index)`), so only a
live client can refresh it. The worker claims a job only while the file's mtime
is at most 5 s old (`Inbox.ClientAlive`, `bridge.ClientHeartbeatFile`), which
closes the window in which a killed client still counts as connected; `/readyz` reports `capture: unavailable` on the same condition.

A runner job runs once. Every failure the runner reports carries `retryable`,
from one classification (`jobs.Retryable` in the render worker): `invalid_blueprint`,
`blueprint_too_large`, `invalid_result`, `result_mismatch`, `analysis_failed`
and `factorio_restarted` are not retryable (the blueprint or its result is at
fault); everything else is. The API retries a retryable phase failure with a new runner job, up to the
phase retry limit, and fails the phase at once otherwise. A new code is
retryable unless it is added to `jobs.Retryable`.

## Change checklist

1. Edit the Lua producer and the Go consumer (client.go / `internal/analysisresult/`) together; regenerate the golden fixtures.
2. Update this file and the exact-shape tests on both sides.
3. If the analysis snapshot changes, change `internal/analysisresult/` (the only Go reader), check `types.ts`, and bump `schema_version` when the change is not backward compatible.
4. If you change chunk, resolution or timeout numbers, update `config.lua` and the Go constants/config together.
