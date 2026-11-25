# Frontend (`apps/expo-web`)

Astro (server output, Node adapter) with Svelte 5 islands only where state
needs them (icon picker, filter rows, preview viewer, JSON tree, drag lists).
Pico CSS + SCSS. Pages are server-rendered reads of the API; nothing
catalog-dependent is prerendered. `pnpm test` runs vitest on pure modules;
user flows are covered by Playwright ([e2e.md](e2e.md)). File map:
[code-map.md](code-map.md).

## API client

`src/lib/api/` ([README](../apps/expo-web/src/lib/api/README.md)):
`ApiClient.api` is openapi-fetch typed by `packages/expo-api-types`, unwrapped
with `ok()`; errors are `ApiError` (`code`, `detail`, `fields`).

- SSR: `getServerApiClient` (base `API_INTERNAL_BASE_URL`), acting as the
  visitor: session cookie as bearer token, `X-Real-IP`.
- Browser: `getBrowserApiClient` (`PUBLIC_API_BASE_URL`, may be `/` for
  same-origin). Never import `astro:env/server` client-side or leak internal
  URLs.
- The edit token is read from the page URL by the clients and sent as
  `X-Edit-Token`; build links between an entry's pages with
  `lib/entry-links.ts` so they carry it.
- Every user-triggered call goes through `runAction` (`lib/action.ts` /
  `browser-action.ts`): busy state, one 401 policy (account pages go to
  `/login`; edit-token flows stay), and the failure text (site text for known
  codes via `explainApiError`, else the API's text). Anonymous writes use
  `runCheckedAction` (Turnstile token, widget reset after each failure).

## Local development

`pnpm dev` in `apps/expo-web` against the compose API. The dev server proxies
`/api` to `API_INTERNAL_BASE_URL` (`vite.server.proxy`) and listens on every
interface, so LAN devices work too.

- `apps/expo-web/.env`: `PUBLIC_API_BASE_URL=/`,
  `API_INTERNAL_BASE_URL=http://localhost:8080`.
- Root `.env`: `API_ALLOWED_ORIGIN` lists every origin you open, the one you
  browse first (it is the site origin: passkeys and sign-in redirects use it);
  `API_COOKIE_SECURE=false` for plain HTTP on a LAN address. Then
  `docker compose up -d expo-api`.

## Pages

| Route | What |
|---|---|
| `/` | Hero search, featured challenge, staff picks, trending (day/week/month tabs), fresh leaderboard records, newest uploads. Each section loads on its own and is left out when empty or failed. |
| `/browse` | Search ([search.md](search.md)). Empty `q` = newest. Advanced filters with facet counts; `?similar_to=` lists similar blueprints. Results are blueprints by default (kind filter adds books and planners). |
| `/e/{id}` | Entry: book tree or inline blueprint; `?revision=`; inline edit (one form) for editors; publish panel for quick previews and edit-link holders; comments. A new anonymous entry opens on its clean link and shows its edit link once. |
| `/e/{id}/b/{blueprintId}` | Leaf: preview (zoom viewer), export, entities/recipes, platform stats, production, schedules, parameters, similar blueprints, Compare button. |
| `/e/{id}/revisions/new` | New revision with an optional note. |
| `/compare?a=&b=` | Two public blueprints side by side (`lib/compare.ts`); `noindex`; 404 for anything else. |
| `/upload` | Tabs: paste string, from file (read in the browser), import from link. Shared details fields (title, description, tags, alias for signed-in users, expiry, spoiler). Inspect runs 800 ms after input and suggests tags; Upload is enabled only for a submittable source. |
| `/preview` | Quick preview of one blueprint. |
| `/json` | Decode/encode exchange strings (`?from={node id}` loads one). |
| `/leaderboard` | Ships / Factories boards (`?group=factories`). |
| `/challenges`, `/challenges/{slug}`, `.../enter` | Challenge list, page (entries, voting, podiums) and upload-and-enter. |
| `/login`, `/signup`, `/recover`, `/recovery-code` | Provider links and passkeys; username step after a new provider sign-in; recovery by username + code; the one-time code display (handed over in `sessionStorage`, never in a URL). |
| `/account` | Sign-in methods, recovery code, sessions, data export, account deletion (behind "Confirm it's you"). |
| `/account/entries`, `/favorites`, `/collections[/{id}]` | The owner's lists; `entries` is the only listing with private and unlisted entries. |
| `/u/{username}[/collections|/comments]` | Public profile tabs; owner edits status/links and arranges pins in place. `/u/{username}/feed.atom` is the Atom feed (SSR route, anonymous read, `ETag`, 5 min cache). |
| `/@{username}/{slug}` | Vanity alias, rewritten to the entry page (canonical stays `/e/{id}`). |
| `/collections/{id}` | Shareable public collection. |
| `/about`, `/privacy-policy`, `/terms-of-use`, `/account-deleted` | Static pages; the XP section of `/about` renders `GET /api/v1/xp-rules`. |
| `/developers/api`, `/openapi.json` | Read API docs, prerendered by `scripts/api-docs.ts`. |
| `/robots.txt`, `/sitemap-index.xml`, `/sitemap-entries.xml`, `/opensearch.xml`, `/.well-known/security.txt`, `/.well-known/passkey-endpoints` | Crawl and discovery files. |

An entry never lands on an intermediate page: the page reads
`revision.root.kind` (book tree or inline blueprint). Node labels never replace
the uploader's title and description.

## Authentication

- The session JWT is an HttpOnly cookie; no script can read it. **The server is
  the only authority on who is viewing; the browser stores nothing about it.**
  The middleware makes one `/api/v1/me` call per request (`lib/viewer.ts`,
  `Astro.locals.viewer`); a rejected session cookie is dropped and the page
  renders anonymous. `Page.astro` sets `data-auth`; CSS shows
  `[data-auth-only]` / hides `[data-guest-only]` from it.
- Edit rights and favorite state are server-rendered too
  (`lib/entry-access.ts` `loadEntryAccess`).
- A session that ends under an open page is noticed by a 401
  (`markSignedOut`).
- **Confirm it's you:** on 403 `reauth_required`, `sign-in-methods.ts` keeps
  the change in `sessionStorage` (`lib/fresh-sign-in.ts`, 10 min), confirms
  with a passkey in the page or a provider round trip
  (`/account?reauth=done`), then retries. Only a page script can store a
  waiting change, so a crafted link changes nothing.
- Social login is plain links to the API's `/auth/{provider}/start`, never a
  provider SDK or One Tap script.

**Turnstile** (when `PUBLIC_TURNSTILE_SITE_KEY` is set): sign-up forms and
every anonymous write form (upload, revision, quick preview, import, entry
report, bug report). `lib/turnstile.ts` loads Cloudflare's script on demand,
and only those pages extend the CSP (`allowTurnstile` for sign-up pages,
`allowGuestTurnstile` for signed-out viewers; the layout does it for every
signed-out page because the bug dialog is everywhere). Signing in never
contacts Cloudflare.

## Security headers and privacy

- `src/middleware.ts`: `X-Frame-Options`, `nosniff`,
  `Referrer-Policy: same-origin`, `Permissions-Policy`, framing CSP; pages
  rendered with a session cookie or `?token=` are `private, no-store`
  (`?token=` also `noindex`).
- Script/style CSP is Astro's (`security.csp`): hashes only, no inline
  scripts (theme bootstrap is `public/theme-init.js`), the Markdown editor's
  runtime style by a build-time hash (`csp-hashes.mjs`). API and icon origins
  come from the `PUBLIC_*` build variables.
- No consent banner: every cookie and storage entry is strictly necessary
  (sessions, flow cookies, `localStorage.theme`, `expo:` `sessionStorage`
  keys). Adding analytics, embeds, remote fonts or other non-essential storage
  needs a banner and a policy update.
- The privacy policy and terms describe how the system works (cookies,
  sessions, export, retention, bans, reports, render queue). Change them with
  those, and bump "Last updated". Operator details come from the
  `PUBLIC_LEGAL` build variable (`lib/legal.ts`), not the repository.

## Search engines

`Page.astro` renders title, description, canonical (`Astro.site` + path, never
the query), Open Graph and `noindex`. Indexed: home, browse (plain and paged),
public entries and leaves, profiles, collections, static pages, upload,
preview, string tool. `noindex`: account and auth pages, new revision,
compare, 404, unlisted, private and quick-preview entries. JSON-LD via `jsonLd`
(`lib/utils/meta.ts`). The entry sitemap pages through
`GET /api/v1/sitemap/entries`, cached 10 minutes in memory.

## Components and scripts

- Pages fetch and compose; components are grouped by domain. Astro by
  default; Svelte only for real client state.
- Client scripts longer than a few lines are a `.ts` next to the component,
  joined through `data-*` hooks.
- A script-submitted form renders its submit button `disabled` until the
  script binds, so an early submit never becomes a GET with the fields in the
  URL.
- Styles live with the markup they target (Astro scoping). Write class names
  literally: `purge-css.mjs` purges unused CSS at build.
- One card component (`EntryCard`, data via `lib/utils/entry-card.ts`) serves
  every list.

## Live progress

A leaf view with a pending phase opens an `EventSource` on the entry's
events (`lib/entry-progress.ts` `watchLeaf`, from
`components/blueprint/live-leaf.ts`) and closes it on `done`/`gone`. On a new
state the page fetches its own URL and swaps the `[data-live-region]` blocks
(`lib/live-regions.ts`); scripts rebind via `onLiveRegions`. No polling: the
page works without live updates. A book page with pending card previews follows the same stream
(`watchPreviews`, `components/entry/tree-live.ts`) and swaps the `tree`
region whenever the set of pending previews changes, keeping open sub-books
open. A re-run started on the page (re-render of
an evicted full preview) dispatches `expo:leaf-requeued` and follows the same
stream.

## Feature notes

- **Preview viewer** (`components/blueprint/viewer/`): zoom/pan on one
  `<img>`, maths in `panzoom.ts` (pure, tested); revision timeline for
  single-blueprint entries (`GET .../history`); author notes as pins
  (rendered with `formatRichText`, never Markdown). Mounted on first open.
- **Comments:** server-rendered by `renderCommentMarkdown` (stricter than
  descriptions, `#N` links); refreshed as a live region. Changing an entry to
  unlisted/private asks first when it has comments.
- **Spoiler entries:** cards blur the preview until a click; entry and leaf
  pages wrap content in `SpoilerGate`; no share image.
- **Planners** have no page: they open in place in a book; never "Open in
  editor".
- **Open in editor** links the raw string URL to the FBE fork at
  `https://fbe.factorygamefan.com/?source=` (public and unlisted only; warns
  about third-party mods; cannot reach a local stack).
- **Drag to game / drop a file:** every place that shows an exchange string (`BlueprintExport`, the entry page's copy row, the string tool's string field) has a drag handle (`lib/drag-string.ts`) that drags the string as `text/plain`, which Factorio accepts on its window. The upload textarea and both string tool textareas accept a dropped text file (`lib/file-drop.ts`), read in the browser as if pasted.
- **Bug button** on every page (`components/layout/ProblemReport.astro`),
  shows what context it sends.

## Conventions

- Rich text (labels, descriptions, comments) goes through
  `@factorio-expo/ui/game/rich-text.ts`; cards use `formatRichTextSummary`.
  Unknown tags stay as written.
- Quality: normal omits `quality=` in tokens; product filters send explicit
  `quality=normal` for an exact match; fluids have no quality.
- Dates only through `@factorio-expo/ui/utils/dates.ts` (Temporal, polyfill on
  demand). No `Date`. The server renders UTC; `<time data-local>` switches to
  local time in the browser.
- Game icons come from generated sprite sheets (see `apps/expo-game-data`
  README); a sheet is fetched only when one of its icons is shown.
- Descriptions keep their Markdown source; rendering and sanitising are
  presentation only.
