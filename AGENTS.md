# AGENTS.md

**Factorio Expo**: public Factorio blueprint hosting, preview and analysis.
Go services (`apps/expo-api`, `apps/expo-render-worker`), Astro + Svelte
frontends (`apps/expo-web`, `apps/expo-admin`), a Lua bridge mod driving real
Factorio (`factorio/`). Anonymous uploads are a core decision; accounts only
add favorites, collections, ownership, private visibility and editing.

Code is the source of truth. If a doc disagrees with the code, fix the doc.

## Commands

Run from the repo root unless noted. `go.mod` is at the root (module `factorio-expo`).

```sh
go build ./... && go test ./...
go test ./apps/expo-api/internal/catalog -run '^TestName$' -count=1
pnpm install                                          # whole JS workspace + git hooks
pnpm lint:check && pnpm format:check && pnpm test && pnpm build
cd apps/expo-web && pnpm dev                          # site against the compose API
cd e2e && pnpm test                                   # Playwright, fresh compose stack (docs/e2e.md)

# After changing an API route (tests fail while any is stale):
go run ./apps/expo-api/cmd/expo-api openapi > apps/expo-api/openapi/public.json
go run ./apps/expo-api/cmd/expo-api openapi admin > apps/expo-api/openapi/admin.json
cd packages/expo-api-types && pnpm generate && pnpm test
```

- PostgreSQL-backed tests run only with `TEST_DATABASE_URL` (catalog migrations
  applied) / `TEST_RUNNER_DATABASE_URL`; use `go test -p 1`. Recipe in the
  README's Development section.
- Lint and format are oxlint/oxfmt via the pnpm scripts (root configs); never
  prettier.
- Git hooks (lefthook) check staged files, require Conventional Commits and
  refuse pushes to `master`.
- Compose: copy `.env.example` to `.env`. Profiles: `tools`, `admin`.

## Docs

Read the one you need. Start with the code map before searching the code.

| Doc | For |
|---|---|
| [docs/code-map.md](docs/code-map.md) | which package/directory owns what |
| [docs/architecture.md](docs/architecture.md) | services, ownership, pipeline, render queue, previews, XP |
| [docs/api.md](docs/api.md) | API rules (auth, abuse controls, imports, raw strings, SSE); routes are in the OpenAPI JSON |
| [docs/bridge-protocol.md](docs/bridge-protocol.md) | Go runner <-> Lua bridge commands and result files |
| [docs/schema.md](docs/schema.md) | tables and invariants |
| [docs/database.md](docs/database.md) | Postgres, sqlc, migrations, roles |
| [docs/search.md](docs/search.md) | Typesense projection and query parameters |
| [docs/frontend.md](docs/frontend.md) | web pages, API client rules, auth, CSP |
| [docs/admin.md](docs/admin.md) | staff console, sign-in gate, deletion and GDPR rules |
| [docs/production-rate-calculation.md](docs/production-rate-calculation.md), [docs/platforms.md](docs/platforms.md) | analysis contract, space platform metrics |
| [docs/e2e.md](docs/e2e.md), [docs/test-data.md](docs/test-data.md) | e2e stack and conventions, fixtures |

## Always

- Update the matching doc when you change a boundary, table, endpoint or
  persistence decision. Keep `docs/code-map.md` and `docs/bridge-protocol.md`
  current in the same change; list new fixtures in `docs/test-data.md`.
- When you change a user-visible flow, run the e2e suite and update
  `e2e/tests/`.
- Every route of both expo-api listeners is a Huma operation
  (`internal/httpapi/openapi.go`, `openapi_admin.go`) with typed input/output
  structs and a `doc` tag on every public response field. Shared enums are
  named Go string types registered once in `httpapi/enums.go`. Errors are
  problem+json with a `code`. Frontends use only the generated types
  (re-exported by each app's `src/lib/api/types.ts`).
- Persist API state and its dispatch intent atomically (`phase_jobs`,
  `entry_search_jobs` are the outbox). Runner contracts are versioned and
  idempotent.
- Treat the Go runner and the Lua bridge as one versioned protocol: compare
  both sides of every request/response (`factorio/factorio-expo-bridge`,
  `apps/expo-render-worker/internal/bridge/`) and test the exact shape.
- Every schema change is a new numbered migration (up + down) in the owning
  app's `migrations/`; seed changes too ([database.md](docs/database.md#migration-policy)).
  Edit sqlc `.sql` queries and regenerate `internal/db/generated/`.
- Keep Go files short and split by responsibility; command packages only wire
  config and start services. Put code both frontends need in
  `packages/expo-ui/src/<domain>/`.
- Naming: deployables, compose services and binaries share the `expo-` role
  name (`expo-api`, `expo-web`, `expo-admin`, `expo-render-worker`); Go
  packages are short lowercase nouns.

## Never

- Expose monotonic ids, preview job ids, service names or storage paths in
  public links. Public identity is `/e/{id}`, a random 12-character id
  (`internal/shortid`). Exception: per-entry comment numbers `#N`.
- Infer ownership or create placeholder users. Anonymous entries stay
  ownerless (`owner_user_id` null); their edit access is the edit token, stored
  only as a hash.
- Let the runner read or write catalog tables, or the API mutate `runner_*`
  tables. Runner state is never public durable data.
- Add public container-control, RCON or bridge-command routes.
- Register a route on a mux by hand, return `map[string]any` or
  `json.RawMessage` (`OpenJSON` is the one documented open object), or edit
  generated files (`openapi/*.json`, `packages/expo-api-types/src/public.d.ts`,
  `internal/db/generated/`, `internal/mods/modsets.json`).
- Edit an applied migration, or run DDL from an app (`expo-db-setup` migrates).
- Store PNG bytes in PostgreSQL or add a new datastore without measured need.
- Interpret a blueprint against an unspecified game version or mod set, or
  let a submission name a render target.
- Use string matching for quality search (use normalized analysed facts).

## Product rules

- Preserve the exact original exchange string and an immutable decoded
  revision. `revisions.game_version` is the version read from the string;
  listings, the API and search all use it.
- Books: the API decodes and traverses the whole submission before
  dispatching, bounded by `blueprint.DefaultLimits`. Every book and blueprint
  is a row; `revisions.tree` holds shape only; every leaf stores a
  self-contained exchange string. Preserve empty books, duplicate labels,
  icons and active indexes. Upgrade and deconstruction planners are tree
  nodes (`planners`): listed and copied, never rendered, analysed or opened in
  the editor. Reject any other item type explicitly. The runner accepts leaf
  blueprints only; book state is derived from leaves and shows partial
  success.
- Comments: one flat thread per entry, signed-in users only, public
  non-quick-preview entries only; leaving public deletes the thread. Removal
  leaves a tombstone. Bans are generic (`user_bans.actions`); IP bans are
  short and store only HMAC hashes of banned accounts' IPs.
- Favorites and collections need an account. Tags apply to entries, not nodes; the tag list is curated.
- Search is Typesense, a disposable projection of PostgreSQL.
  Production-rate filters carry quality and use the pinned content set.

## Runtime images

Go services build static (`CGO_ENABLED=0`) on `golang:*-trixie` and run on
`gcr.io/distroless/static-debian13`; `expo-api` runs as `nonroot`,
`expo-render-worker` as root (shared volumes with Factorio) with the
`healthcheck` subcommand as its compose healthcheck. `expo-web`/`expo-admin`
run on `gcr.io/distroless/nodejs26-debian13:nonroot` under the Node
permission model (no file writes, child processes or workers). Images that
need packages use `debian:trixie-<date>-slim` and run `apt-get upgrade -y`.
Pin base images to exact patch tags (Go also via `toolchain`); rebuild with
`docker compose build --pull`.
