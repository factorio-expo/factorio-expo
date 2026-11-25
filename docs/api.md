# HTTP APIs

The Go code is the definition. Every route of expo-api is a Huma operation;
the generated OpenAPI documents are the per-route reference (paths, fields,
status codes):

- Public listener: `apps/expo-api/openapi/public.json` (also published by
  expo-web at `/openapi.json` and `/developers/api`, minus `x-internal` routes).
- Staff listener: `apps/expo-api/openapi/admin.json`.
- Runner: `internal/runnerapi/` and `apps/expo-render-worker/internal/httpapi/`.

This file holds the rules a route listing does not show. Config: every `API_*`
variable is parsed in `apps/expo-api/internal/config/` (defaults there and in
`.env.example`).

## Public API (`expo-api`)

Every route is under `/api/v1` (`publicPrefix`); a breaking change ships as
`/api/v2`. `/healthz` is outside it. There is no listing endpoint for browsing:
browse is `GET /api/v1/search` with an empty `q`.

### OpenAPI and its tests

- Each operation's typed input/output structs, `doc`/`enum` tags and summary
  are its contract. Huma validates requests; unknown query parameters and body
  fields are refused (422 `validation_failed`).
- `s.operation(op, access, limit, gates...)` sets access (`public`,
  `optional`, `required`, `fresh`), rate-limit class and gates.
- Tests: `TestRoutesAreHumaOperations` (no hand-registered routes),
  `TestOperationsAreDocumented`, `TestPublicSpecDocumentsEveryField`,
  `TestOpenAPISpecIsCurrent` (regenerate on change, see AGENTS.md),
  `TestSearchParametersMatchFilterTable`.
- Shared enums are named Go types registered once in `httpapi/enums.go`.
- Responses never carry `null` for a list or object (`emptyslices.go`).
- Routes not marked `x-internal` (search, entries, leaf with analysis, raw
  strings, preview images, tags, modsets) are the public read API tools may
  rely on; everything else may change.

### Errors

Every error (public and staff) is `application/problem+json`
(`httpapi.Problem`): `status`, `title`, `detail` (for people), `code` (stable,
for clients). Validation failures are 422 `validation_failed` with an
`errors` list of `{location, message}`; framework errors get a fixed plain
`detail`. Domain errors (`fault.Error`) carry their own code (`not_found`,
`render_queue_full`, `banned`, ...); a 500 is `internal` with details only in
the log. Some codes add fields (`preview_failed` adds `failure_code`). Codes
the web words itself: `ExplainedErrorCode` in `apps/expo-web/src/lib/api/types.ts`
(texts in `lib/api-errors.ts`).

### Sessions and sign-in

- Accounts have no password and no email. Sign-in is a provider (GitHub,
  Discord, Google via OAuth 2.0 code + PKCE S256; Steam via OpenID 2.0 with
  `check_authentication`) or a passkey (user verification required).
- Every sign-in starts a server-side session (`user_sessions`); the JWT
  carries only its id (`sid`) and every request looks the row up, so ending a
  session revokes its token at once. Cookie: `__Host-expo_session`
  (HttpOnly, Secure, SameSite=Lax, 7 days; `expo_session` when
  `API_COOKIE_SECURE=false`). `Authorization: Bearer` is also accepted (API
  clients, expo-web SSR). The token is never in a response body.
- Ceremony state rides in signed one-use flow cookies (`expo_oauth` 10 min,
  `expo_signup` 10 min, `expo_passkey` 5 min), keyed from `API_JWT_SECRET` but
  never valid as a session. Passkey challenges are also remembered in memory
  against replay. A mismatched provider callback leaves `expo_oauth` in place,
  so a forged callback cannot cancel a real sign-in.
- A pending sign-up is dropped when a new sign-in starts, on logout and when
  `/login` renders.
- Identities match by provider + provider user id only, never email or name.
- Optional-auth routes: a stale cookie is dropped and the request continues
  anonymously; a bad bearer token is 401.
- Anonymous edits use the edit token (`?token=` or `X-Edit-Token`).

**Fresh sign-in.** Linking a provider, adding/removing a sign-in method, a new
recovery code, the data export and account deletion need a sign-in ceremony
on this session within 10 minutes (`user_sessions.auth_time`), else 403
`reauth_required`. The person confirms with a passkey
(`/api/v1/me/reauth/passkey/*`) or a provider (`start?reauth=1`); it must be
the same account, and it refreshes `auth_time` without a new session.

**Recovery codes.** The way back after losing every sign-in method. 128 random
bits, 26 base32 characters in groups of five; only a domain-prefixed SHA-256
is stored (`users.recovery_code_hash`). Returned only by sign-up, recovery and
`POST /api/v1/me/recovery-code`. `POST /api/v1/auth/recover` signs in with
username + code and swaps the code atomically (works once under concurrency).
Every failure is one 401 `recovery_failed`.

**Sign-up guards** (`signup_guard.go`), cheapest first: sign-in and
account-creation rate limits (looked at, not drawn), flow cookie and username,
Turnstile (when `API_TURNSTILE_SECRET_KEY` is set; checks `success`, action
and host; 403 `turnstile_failed`, 503 `turnstile_unavailable`), and only once
the account exists is the creation budget charged. Signing in has no
Turnstile. Cloudflare's test secrets check `success` only, so localhost works.

### Entries and visibility

- `public`: listed on the owner's profile and searchable. `unlisted`:
  readable by anyone with the link, never listed or searched. `private`:
  owner only; everyone else gets 404, writes included. Unlisted and private
  need an owner. Changes take effect at once everywhere.
- Quick previews (`quick_preview: true`, always with `expires_at`): readable
  until expiry, never listed or searchable, read-only (409) until published.
- Expiring uploads (`expires_in_days` 1/7/14/30): 404 once expired; the
  minute sweep soft-deletes them.
- Publish (`POST .../publish`) claims a quick preview or anonymous entry for
  the signed-in caller (owner or edit-token holder): sets the owner, clears
  the edit token and quick-preview flag, reuses the renders.
- Limits: submission body 8 MiB; tree limits `blueprint.DefaultLimits` (8 MiB
  decoded, depth 16, 2000 nodes, 100 leaves); search page max 100.

### Abuse controls

- **CSRF:** a non-GET request whose `Origin` is not in `API_ALLOWED_ORIGIN`,
  or with `Sec-Fetch-Site: cross-site`, is 403 before routing.
- **Rate limits** (`httpapi/ratelimit.go`: every rule's burst and interval;
  429 + `Retry-After`): anonymous callers by IP (IPv6 /64), signed-in by
  account, some rules both. No budget is site-wide (a flood could lock
  everyone out). Comments also have daily caps in the database. Client IP:
  `API_CLIENT_IP_HEADER` only from `API_TRUSTED_PROXIES`.
- **Render queue cap:** 429 `render_queue_full` when an upload would pass the
  uploader's waiting-leaf cap ([architecture](architecture.md#render-queue)).
- **Turnstile on anonymous writes** (`human_check.go`): upload (`upload`),
  import (`import`), edit-token revision (`revision`), quick preview
  (`quick-preview`), entry report (`report`), problem report
  (`problem-report`). Checked after the rate limit and field validation.
  Signed-in callers never need it.
- **Paused actions** (`switches.go`, console settings): 503 +
  `Retry-After: 60` with `uploads_paused`, `anonymous_uploads_paused`,
  `signups_paused`, `comments_paused`. Reads, sign-in, edits, deletes, reports
  and work already under way are never paused.
- **Bans** (`user_bans`): generic per action (`comment`, `upload`, `report`),
  checked by the catalog operations (403 with reason and end). When a banned
  account tries a blocked action its IP is banned as an HMAC hash for
  `API_IP_BAN_DURATION` (default 6 h, max 24 h). No IP data is stored for
  anyone not banned, except an open anonymous report's hash.

### Comments, reports, notes

- One flat thread per entry; only live, public, non-quick-preview entries;
  signed-in posters. Leaving public deletes the thread and its reports.
  `number` (`#N`) is per entry and never reused. Body: Markdown, at most 4000
  characters and 60 lines, rendered by expo-web. Removal leaves a tombstone
  (`removed_by`: `author`, `owner`, `admin`, `account`).
- Entry reports: open to anyone who can see the entry (not its editors, not
  quick previews), one open report per reporter. Reports never hide anything;
  staff decide. `stolen` needs a note (the takedown path).
- Problem reports (the bug button): same rules; the contact email is cleared
  on resolve or after 30 days; resolved reports are deleted 7 days later.
- Notes: the editor's pins on a leaf preview (`x`, `y` fractions, rich text,
  500 characters, 10 lines, 20 per leaf); live-revision leaves only; the
  number is the rank by creation.

### Profiles, leaderboards, challenges

- **Profiles** show only listed entries: status (140 chars, one line), social
  links from a fixed site list (the API builds the URL from the name), up to 6
  pins, stats, and progress (XP, level, badges, held records). No "last
  active" time, no ranking of users.
- **Leaderboards** (`catalog/leaderboards.go`): ships (`fastest`,
  `thrust_to_weight`, `lightest`, `most_cargo`) and factories
  (`most_science`, `densest_science`, `cleanest_science`,
  `biggest_power_plant`). Ranked from PostgreSQL, not search; only current
  revisions of listed entries with a `complete` analysis and built-in mods
  only; one place per entry; ties to the earlier revision; hidden until 10
  entries; cached 30 s. The `leaderboards` task stores each leader in
  `board_leaders` (no history) and awards record XP. Cards carry `records`
  from one `board_leaders` read per listing, never from the index.
- **Challenges** (made in the console): a goal and restrictions over one
  parameter catalogue (`internal/challenge`), optional import rule; base or
  Space Age only; one active submission per user; status follows the times
  (results publish themselves at `voting_ends_at`). Scores are computed on
  read from stored facts (cached 10 s). Leaving public withdraws an entry for
  good (trigger). Votes: `votes_per_user`, never one's own, hidden until
  closed.

### Import from other sites

The upload form may give a link; `POST /api/v1/imports/resolve` fetches it
through `internal/importer` (the only code that fetches a user's URL), checks
it parses as a blueprint (else 422 `import_no_blueprint`, nothing echoed) and
answers the string, its inspection and a `source_token` (HMAC over canonical
source, string hash and a 30-minute expiry). The upload then goes through
`POST /api/v1/entries` with `source_url`, `source_token` and
`import_consent: true`; a missing or foreign token is 422
`import_source_unverified`.

- Sites: one fixed adapter each in `importer/sites.go` (pastebin, hastebin,
  gist, raw.githubusercontent, gitlab raw, FactorioBin, Google Docs export,
  Factorio Codex, Factorio Prints). The request is built from the adapter's
  base and a validated id, never from the pasted URL.
- Fetcher security: https only; no userinfo, port or IP literal; exact host
  match; connect-time address check on the resolved address (no loopback,
  private, link-local, CGNAT, metadata...; IPv6 only in 2000::/3; port 443
  only); no proxy, cookies or credentials; 10 s timeout; size cap; redirects
  only to known hosts; expected media type; at most 8 at once.
- Third-party quotas: strict anonymous per-IP budget, per-site hourly budgets
  (503 `import_site_busy`), 5-minute cache of successful resolves.
- `API_IMPORT_STUB_ORIGIN` (local/e2e only, refused unless every origin is
  loopback) points every adapter at a stand-in server.

### Read routes worth knowing

- **Raw strings** (`raw.go`): `/entries/{id}/raw` (live revision, 5 min
  cache), `/entries/{id}/revisions/{rid}/raw`, `/blueprints/{bid}/raw` (1 day).
  Credentials ignored; only live public/unlisted entries and unexpired quick
  previews, else 404 for everyone, so every 200 is shared-cacheable. `max-age`
  is cut to the expiry. Open CORS (`*`, no credentials), own `raw-ip` budget.
- **Preview images:** `/blueprints/{id}/preview.webp` and `thumbnail.webp`:
  ETag, Range, `public, max-age=86400` (private: `private, no-store`). An
  evicted full file is 404 `no-store`; `POST .../preview/render` queues a
  re-render as the requester's work.
- **String tools:** `POST /blueprints/decode`, `encode`, `inspect` store and
  dispatch nothing; inspect returns counts, suggested tags, mod detection and
  `compat_21`.
- **Progress events:** `GET /entries/{id}/events` is SSE (`snapshot`,
  `phase`, `done`, `gone`; statuses `pending`, `ready`, `failed` with a
  failure `code`). Capped at 8 streams per IP and 2000 total; pings every
  20 s; re-reads PostgreSQL every 30 s; ends after 30 min. No polling
  endpoint. Status follows `progress.LeafOf`: an open job is `pending`, else a
  stored result is `ready`, else a failed job is `failed`.
- **Vanity aliases:** `/@{username}/{slug}` resolved via
  `GET /users/{username}/aliases/{slug}`; owners only, one per entry, unique
  per user, no history or redirect when changed.
- **Account data export:** `GET /me/export` (fresh) streams one JSON document
  (`dataexport`, format version in `models.go`); bans, staff notes, views and
  images are left out.

### Headers

All responses: `nosniff`, `X-Frame-Options: DENY`, CSP `default-src 'none'`,
`Referrer-Policy: no-referrer`, `Cache-Control: private, no-cache` unless a
handler overrides it.

## Staff API (expo-api admin listener)

Second listener on `API_ADMIN_ADDR` (`:8081` in compose), never published or
routed by the reverse proxy; unset means no staff routes at all. Every route
but `/healthz` needs `Authorization: Bearer <API_ADMIN_TOKEN>` (>= 32 chars,
constant-time compare) and `X-Staff-User` (the email expo-admin verified;
1-64 printable chars). 401 bad token, 400 no user. No roles, no CORS.

Every write records an `admin_actions` row in the same transaction (cleanups
and job re-runs before they act; cleanups also record the result). `reason`
is at most 500 characters. Routes: `admin.json`; pages: [admin.md](admin.md).

## Runner API (`expo-render-worker`, internal)

| Route | Purpose |
|---|---|
| `POST /v1/preview-jobs` | Job for one leaf, optional phase selection. `Idempotency-Key` dedups repeats of one dispatch attempt. |
| `GET /v1/preview-jobs/{id}` | Job and per-phase state. A job runs once; every failure is final and carries `retryable`. The API retries only those, with a new job. |
| `GET`/`DELETE .../artifact`, `.../analysis` | Fetch / release the temporary PNG or analysis. |
| `GET /healthz`, `/readyz` | Liveness / readiness (Postgres, dirs, RCON, bridge protocol, client); `/readyz` is 200 or 503 with the same body. |

Config: `RUNNER_*` in `apps/expo-render-worker/internal/config/`.
