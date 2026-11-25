# Code map

Which directory owns what, so you can open the right package first. One line
per package; the code and its doc comments hold the detail. Update the line
when you add, move or repurpose a package. Contracts: [api.md](api.md),
[bridge-protocol.md](bridge-protocol.md).

Request flow: `httpapi` handler -> `catalog.Service` (one transaction, sqlc
queries, intents in `phase_jobs` / `entry_search_jobs`) -> `preview.Dispatcher`
-> `runner.Client` -> runner HTTP -> `store` queue -> `worker` -> `bridge.Client`
(RCON) -> Lua `bridge.lua` / `job.lua` -> result files -> `artifacts` -> API
import (`preview`) -> `search.Dispatcher` -> Typesense.

## Shared Go packages (`internal/`)

| Package | Owns |
|---|---|
| `analysisresult` | The only parser of the runner's analysis payload and the stored production snapshot (`Parse`, `Snapshot.View`, `Snapshot.Facts`, build cost). Used by expo-api and the runner. |
| `runnerapi` | Runner HTTP wire contract: paths, `Idempotency-Key`, job/phase/artifact types, readiness body. |
| `icontoken` | Rich-text icon grammar `[item=x,quality=rare]`. TS twin `packages/expo-ui/src/game/icon-token.ts`; both tested against `test-data/icon-tokens.json`. |
| `shortid` | Random 12-character ids of every catalog row and public URL. |
| `modtable` | Mod name table wire shape (written by expo-game-data, embedded by expo-api `internal/mods`). |

## `apps/expo-api/`

`cmd/expo-api/`: wiring only (`main.go`), plus subcommands `healthcheck`,
`openapi [admin]`, `replay-deletions`; `maintenance.go` is the one table of
scheduled housekeeping tasks; `signin.go` builds the sign-in providers.
`openapi/public.json`, `admin.json`: generated OpenAPI documents (checked in).
`migrations/`: catalog schema ([database.md](database.md#migration-policy)).

| `internal/` package | Owns |
|---|---|
| `config` | Every `API_*` env var -> typed `Config` with defaults and cross-checks. No other package reads the environment. |
| `httpapi` | HTTP layer only. Every route is a Huma operation: `openapi.go` (public API, access levels, rate-limit classes, gates), `openapi_admin.go` + `admin_*.go` (staff listener), `enums.go`, `errors.go` (problem+json), `ratelimit.go`, `security.go` (headers, CSRF), `raw.go` (raw strings, own mux and CORS), `human_check.go` / `signup_guard.go` (Turnstile), `switches.go` (pause switches), `auth_*.go` / `fresh_sign_in.go` / `session.go` (sign-in and sessions). One file per resource otherwise. |
| `catalog` | The domain: entries, revisions, tree hydration, access (`access.go`: every entry read/write goes through `entryAccess`), lifecycle rules (`lifecycle.go`, SQL twins in `000001_init`), the write unit (`change.go`: search sync, XP, cache purge, progress after commit), result reuse, quick previews, publish, comments, reports, bans, profiles, pins, collections, favorites, tags and tag suggestions, mod detection, cards, leaderboards, XP ledger, challenges, notes, aliases, feed, trending. `unit.go` is the surface for `staffops` and `accountdeletion`. |
| `staffops` | Staff console operations on top of `catalog.Unit`, each with its `admin_actions` audit row. |
| `audit` | Audit log rows and reason rules. |
| `accountdeletion` | Account soft delete (owner and staff), 7-day purge, the deleted-accounts ledger and its replay. |
| `accounts` | Accounts and sign-in methods (identities, passkeys, recovery codes), username availability. |
| `identity` | Sign-in providers: OAuth 2.0 + PKCE (GitHub, Discord, Google), Steam OpenID (`steam/`). Adding a provider is one entry in `providers.go`. |
| `passkey` | WebAuthn ceremonies (go-webauthn). |
| `sessions` | Server-side sessions (`user_sessions`), fresh sign-in window. |
| `auth` | Session JWT and signed flow tokens, key ring, username rules, secret validation. |
| `banhold` | Keyed hashes of a deleted banned account's username and identities. |
| `turnstile` | Cloudflare Turnstile siteverify client. |
| `ratelimit` | In-memory keyed token buckets. |
| `challenge` | Challenge rules, parameter catalogue, checking and scoring (no database). |
| `importer` | Import from other sites: the only code that fetches a user-supplied URL (adapter table, address guard, budgets, cache). |
| `blueprint` | Pure exchange-string code: decode/encode, bounded tree parse (`DefaultLimits`), book re-encode, planners, display data, summary, 2.1 compatibility (`compat.go`). |
| `mods` | Mod detection against the embedded `modsets.json` (generated; never edit). |
| `progress` | Leaf phase rule (`LeafOf`), progress wire types, in-process `Hub` for SSE. |
| `outbox` | Generic outbox queue: claim under lease, retry/backoff policy, `Nudge`. |
| `preview` | Preview/analysis dispatcher on `outbox`: dispatch, poll, background import, file storage and sweep, storage budget eviction, `Requeuer` (the one re-run entry point), on-demand re-render. |
| `renderqueue` | Render queue policy: tickets per kind of work, `Place` (order), `Check` (per-uploader cap). |
| `previewimage` | PNG -> lossy WebP full + thumbnail via the `cwebp` binary. |
| `storage` | Durable file storage interface and local filesystem backend. |
| `runner` | HTTP client for the runner API. |
| `jobs` | Staff Failed jobs: list and re-run. |
| `maintenance` | Task registry and scheduler; staff cleanups with dry run. |
| `search` | Typesense projection: documents, schema/versioning, index build and alias switch, keeper, filter table and query building, facets, similar, synonyms, view-count refresh, `entry_search_jobs` dispatcher. |
| `views` | Page-view counter (daily-seeded in-memory hashes), bot filter, flusher. |
| `settings` | Global settings definitions, validation, 5 s cache. |
| `staff` | Staff identity check for the admin listener (shared token + `X-Staff-User`). |
| `dataexport` | The account data export (streamed JSON). |
| `edgecache` | Cloudflare cache purge for hidden entries (no-op when unconfigured). |
| `fault` | The one domain error type (`Kind`, `Code`); `httpapi.writeError` maps it to a status. |
| `db` | pgx pool, `queries/*.sql` -> `generated/` (sqlc, checked in, never edit). |

## `apps/expo-render-worker/`

| Path | Owns |
|---|---|
| `cmd/expo-render-worker/` | Wiring and the `healthcheck` subcommand. |
| `internal/config` | `RUNNER_*` / `FACTORIO_*` env. |
| `internal/httpapi` | Runner HTTP API, readiness, retention loop. |
| `internal/jobs` | Job domain types; `Retryable` (the one failure classification). |
| `internal/store` | Postgres queue in the runner's own database (`runner_*`). |
| `internal/worker` | FIFO loop: claim while the bridge is ready, submit, poll, settle phases (`runwatch.go`, `phase.go`). |
| `internal/bridge` | RCON command encoding and strict reply decoding; protocol constants. `bridgetest/` fake. |
| `internal/rcon` | RCON transport. |
| `internal/artifacts` | Inbox reading and validation, retained artifact store, PNG checks. |
| `migrations/` | `runner_jobs`, `runner_artifacts`. |

## `apps/expo-web/` (Astro SSR + Svelte islands)

| Path | Owns |
|---|---|
| `src/pages/` | Routes ([frontend.md](frontend.md#pages)). Pages load data and compose components only. |
| `src/layouts/Page.astro` | Shell: head/SEO tags, header, sidebar, footer, banner, bug button. |
| `src/middleware.ts` | Viewer resolution, session cookie drop, security headers, page caching. |
| `src/components/<domain>/` | Grouped by domain: `entry`, `blueprint` (leaf view, preview viewer, production), `search`, `submission`, `entry-details`, `auth`, `account`, `profile`, `collection`, `challenge`, `home`, `planner`, `tools`, `preview`, `layout`, `common`, `form`. Scripts sit next to their component as `.ts`. |
| `src/lib/api/` | Typed API client ([README](../apps/expo-web/src/lib/api/README.md)); `types.ts` re-exports generated types. |
| `src/lib/search/` | Browse URL <-> typed state <-> API query. |
| `src/lib/` | Pure helpers with unit tests (`*.test.ts`): actions and error texts, entry links and access, live progress and live regions, auth helpers, legal, SEO meta, cards, etc. |
| `scripts/api-docs.ts` | Builds `/openapi.json` and `/developers/api` from `public.json` (into `src/generated/`, not checked in). |
| `purge-css.mjs`, `public/theme-init.js`, `public/third-party/` | Build-time CSS purge, pre-paint theme script, vendored assets with licences. |

## `apps/expo-admin/` (staff console, Astro SSR)

Talks only to expo-api's admin listener ([admin.md](admin.md)).
`src/middleware.ts` (CSRF, proxy JWT check via `lib/access.ts`, dev bypass),
`src/lib/api/` (staff client), `src/pages/` (one page per console section,
`partials/` for polled fragments), `src/components/`, `src/lib/` (form helpers,
bans, Dozzle reader, challenge editor state).

## `packages/`

| Path | Owns |
|---|---|
| `expo-ui/` | Code shared by both frontends, consumed as source: `src/api/` (`ApiError`, `ok`), `src/game/` (icons, sprite sheets, icon tokens, rich text, icon picker, Astro integration serving `game-icons/`), `src/forms/` (`FilterRows`, Markdown editor), `src/styles/`, `src/utils/` (dates via Temporal, dropdown, security headers). No `peerDependencies` on purpose (they break `astro dev`). Generated, git-ignored: `game-icons/`, `src/game/data/`. |
| `expo-api-types/` | TypeScript types generated from the two OpenAPI documents (`pnpm generate`; `./public`, `./admin`). Pins TypeScript 6 (openapi-typescript needs the compiler API). |

## Other

| Path | Owns |
|---|---|
| `apps/expo-db/` | DB tooling image: `setup.sh` (roles, databases, migrations, grants), `migrate.sh`, `lib.sh`, `postgres/` server config. |
| `apps/expo-game-data/` | One-off generator of icon sheets, icon picker data and the mod name table; `communitynames/` hand-written search synonyms (embedded by expo-api). See its README. |
| `factorio/factorio-expo-bridge/` | Lua bridge mod: `scripts/bridge.lua` (commands), `job.lua` (tick state machine), `blueprint.lua` (placement), `capture.lua`, `analysis.lua` + `rate-calculator/`, `build-cost.lua`, `envelope.lua` (result files), `tests/`. |
| `factorio/factorio-{server,client,downloader}/` | Images and entrypoints; the client has the restart watchdog and an `LD_PRELOAD` FPS cap (`fpslimit/`). |
| `e2e/` | Playwright suite ([e2e.md](e2e.md)): `tests/` one spec per area, `support/` (stack control, seeding via the public API, passkeys), `import-stub/`, `recovery/`. |
| `test-data/` | Fixtures ([test-data.md](test-data.md)). |
| `docker-compose.yaml` | The stack; profiles `tools` and `admin`. |
| `lefthook.yml`, `.lefthook/`, `commitlint.config.js` | Git hooks. |
