# End-to-end tests (`e2e/`)

Black-box Playwright tests of the product through a browser: what a visitor can
see and do, not how the services do it. They run against the real compose stack
(the same images as production), started as a separate compose project.

## Running

Needs Docker, Node 26+ and pnpm. `pnpm install` at the repository root installs
the whole workspace; then from `e2e/`:

```sh
pnpm exec playwright install chromium   # once per machine
pnpm test                                # builds a fresh stack, runs, tears it down
```

`E2E_STACK` controls the stack lifecycle around a run:

| Value | Stack |
|---|---|
| unset | fresh stack (old one and its data removed first), torn down afterwards |
| `keep` | fresh stack, left running for debugging (`pnpm stack:logs [service]`) |
| `reuse` | an already running stack (`pnpm stack:up`), left running |

`pnpm stack:up` / `pnpm stack:down` manage the stack by hand; with
`E2E_STACK=reuse` the suite reruns in seconds instead of rebuilding images.
`pnpm test:ui` opens the Playwright UI; failures keep a trace and a screenshot
(`pnpm report`).

Lint and format with oxlint/oxfmt like the rest of the workspace (`pnpm lint:check`,
`pnpm format:check`, `pnpm typecheck`; `pnpm lint` / `pnpm format` fix).
`e2e/.oxlintrc.json` extends the root `.oxlintrc.json`, with
`no-await-in-loop` off (steps and polls run in order on purpose) and
`no-unsafe-type-assertion` off (as for the apps' tests); the root
`.oxfmtrc.json` skips all YAML, which keeps the compose files' 2-space style.

## The stack (`e2e/compose.yaml`)

`docker compose -p factorio-expo-e2e --env-file e2e/e2e.env -f docker-compose.yaml -f e2e/compose.yaml`:
the production compose file plus an override, so it never touches the dev
stack's containers, volumes or ports. `e2e/e2e.env` holds throwaway secrets;
the developer's `.env` is not read.

- **Own database:** Postgres and Typesense keep their data in tmpfs, so every
  stack starts empty and freshly migrated by `expo-db-setup`. Tests may change
  anything. The API's file storage is a tmpfs too, so no host directory is
  written.
- **No Factorio:** server, client and downloader are behind an unused profile
  (they need a Factorio account and minutes of CPU per render). The render
  worker still runs and accepts jobs, so previews stay "rendering"; tests do not
  assert preview images or analysis results. Phases settle only by failing:
  `live-progress` uploads a blueprint over the worker's request limit while the
  worker is paused (`pauseRenderWorker`), so the page is open before the
  refusal arrives. The recovery suite adds Factorio
  (below).
- **Rate limits off** (`API_RATE_LIMIT=false`): one IP registers dozens of
  accounts per run.
- **Render queue wide open** (`API_RENDER_WINDOW`, `API_RENDER_CAP_*` 100000):
  no render finishes without Factorio, so the suite's leaves pile up; the
  window keeps every leaf sent to the worker (`live-progress` needs its
  refusal) and the caps keep every upload accepted. The cap and the order are
  covered by the API's Go tests.
- **Turnstile off** (`API_TURNSTILE_SECRET_KEY` and the web's
  `PUBLIC_TURNSTILE_SITE_KEY` empty): sign-up forms render no widget and the
  API checks no token. The Go tests cover the check against a fake
  `siteverify` (`internal/turnstile`, `httpapi/signup_guard_test.go`).
- **Import stand-in** (`expo-import-stub`, `e2e/import-stub/server.mjs`, loopback port `E2E_IMPORT_STUB_PORT`, 8190): the API runs with `API_IMPORT_STUB_ORIGIN` (refused unless the whole stack is local and `API_COOKIE_SECURE` is false, which is why this stack serves plain session cookies), so every import adapter fetches from `http://expo-import-stub:8190/<adapter>` and may reach the private network; specs register answers with `stubSite` (`support/import-stub.ts`). The fetcher's real address and host checks are covered by its Go tests (`internal/importer`).
- **Passkeys only:** no sign-in provider is configured (they need real
  provider apps; the API's Go tests run the provider flows against fakes).
  Every account signs up with a passkey on Chromium's virtual authenticator
  (`support/passkeys.ts`, CDP `WebAuthn.addVirtualAuthenticator`): the
  Seeder's accounts in a throwaway browser context, UI flows on the test's own
  page (`useVirtualAuthenticator(page)` before the passkey prompt). The
  authenticator always verifies the user (`isUserVerified: true`): the API
  requires user verification. Every sign-up now ends on `/recovery-code` first: UI specs use
  `signUpWithPasskey(page)` (`support/passkeys.ts`: sign up, read the code, press
  Continue) unless they test the sign-up itself, and the Seeder's accounts carry
  the `recoveryCode` their sign-up showed. Passkeys are bound to `localhost`, so the suite never uses `127.0.0.1`. Search and preview pollers run every second.
- **Ports:** web `http://localhost:3100`, API `http://localhost:8180`
  (`E2E_WEB_PORT` / `E2E_API_PORT`; the web image bakes the API URL in at build
  time). The session cookie stays `Secure` + `__Host-`, which browsers accept on
  `localhost`.

## Recovery suite (`recovery/`, with Factorio)

`pnpm test:recovery` (`recovery.config.ts`) proves the stack heals from
Factorio failures without anyone stepping in: the server and the client each
crash (SIGKILL of the `factorio` process, the container keeps running) and
restart (`docker restart`, SIGTERM; not a compose deploy, so the worker keeps
running), while idle and while a job runs, and the server goes away for a
while. After each, the client must be back in the game on its own and a
blueprint uploaded around the failure must get its preview and analysis through
the public API sooner than the runner's 5 minute job timeout would allow. The
job running when the server goes down instead fails for good with
`factorio_restarted` in both phases, and the next upload renders; a client lost
after the capture reruns only the preview.

- The same `factorio-expo-e2e` project with `factorio.compose.yaml` layered on
  (`E2E_FACTORIO=1`, set by the config), so it replaces a running e2e stack;
  `E2E_STACK` works as above (`E2E_FACTORIO=1 pnpm stack:up` for `reuse`).
- No download: server and client mount the dev stack's engine volume
  (`factorio-expo_factorio-engine`; start the dev stack once) or the volume
  named by `E2E_FACTORIO_ENGINE_VOLUME`, which must hold `FACTORIO_VERSION`.
  The client's sprite-atlas cache is the external volume
  `factorio-expo-e2e-client-cache`, kept across runs (the first run builds it).
- Tests run one at a time (they crash shared containers) and take minutes each;
  timing comes from Factorio's own log lines (client state changes, bridge
  progress lines; `recovery/factorio.ts`). The mid-job blueprint is
  `test-data/ship-hauler.txt` (`PLATFORM`), slow enough to act on a step while it
  runs.
- Not part of `pnpm test`; nothing runs it automatically.

## Writing tests

- `support/fixtures.ts` is the `test` to import. Option `viewer` is `"guest"`
  (default) or `"member"`; a member test gets a fresh account (`me`), already
  signed in. `me.api` and the page are two sessions of that account (two
  devices): logging out on the page leaves `me.api` signed in. Flows that
  matter to both run in a `for (const viewer of viewers)` loop.
- Setup steps outside the API (`support/stack.ts`, straight in the database):
  `ageSignIns(username)` moves an account's logins an hour back, so the next
  sign-in method change or data export asks "Confirm it's you";
  challenge staff work (the console is not in the e2e stack):
  `createChallenge(slug, title, settings)` publishes one in any phase with any
  option (or a draft), `setChallengePhase` moves its times to upcoming, open,
  judging or closed as time would, `overrideChallengeEntry` sets a staff
  verdict, `ageRevisions` / `ageAccount` backdate an entry's revisions or a
  sign-up; `storeAnalysis(entryId, {width, height, partial?, rate?, platform?, science?})`
  stores the analysis fixture with that footprint (`rate` multiplies its
  0.5/s of electronic circuits, `platform` makes it a space platform, `true`
  a slow one or `{speed, thrust, weight}` a ship of its own for the record
  boards, `science` packs a second its only product, for the factory boards;
  and
  `failAnalysis` fails one), so challenge scores and ranks, similar blueprints
  and the compare page are testable without Factorio. `pickForHome(entryId,
  caption)` makes the entry's blueprint the home page's only pick (the
  console's Home picks). Such a write queues no
  search sync, so `reindexEntry(entryId)` does (`setEntryCreatedAt` moves an
  upload date and reindexes, for the date range filter). The API reuses an
  identical exchange string's stored analysis for another leaf, so every such
  entry gets a string of its own (`relabel`).
- Arrange data through the public API with the `seed` fixture
  (`support/api.ts`: accounts (real passkey sign-ups), entries, revisions, collections, favorites,
  `searchable()` to wait for the search projection). The flow under test always
  goes through the UI.
- Every test creates its own uniquely named data (`uniqueWord()`), so tests run
  in parallel and against a reused stack. Never depend on what else is in the
  database (for lists, search for your own unique word).
- Select by role, label and visible text. The header's links are exposed as
  buttons (`header(page)`); the upload form's submit is `submitUpload()`.
- Script-driven buttons (inline edit, revision picker, favorites, collections)
  ignore clicks that land before their script runs: after a click navigates,
  call `ready(page)` (or a `support/ui.ts` helper that does) before using them.
  Form submit buttons stay disabled until their script runs, which Playwright
  waits for on its own. `holdScripts(page)` simulates a slow connection.
- Blueprints come from `test-data/` (`support/blueprints.ts`); `relabel()` gives
  an upload a recognisable label. The default `BLUEPRINT` is vanilla 2.1; tests
  about Space Age content, built-in mods or a 2.0 game version use
  `SPACE_AGE_20`, built in `support/blueprints.ts` like `blueprintOf()`. Tests
  never hard-code what the `PLATFORM` ship suggests or contains: it is swapped
  for other ships.
- A bug the suite finds gets fixed, with a test of the fixed behaviour; do not
  pin it with `test.fail`.

## Coverage

| Spec | Flows |
|---|---|
| `theme` | dark/light toggle, persistence, system preference, signed in |
| `home` | the search box opens `/browse` with the query, the Advanced search link opens the filters, the sidebar's Home and Browse, newest uploads, trending (views from three visitors; the tabs switch panels with the keyboard and no API request; an unknown period refused), the featured open challenge, one `h1` with sections as `h2` and cards as `h3`, indexed with a `SearchAction`; staff picks with their caption inside the card in the book row's look, gone once the entry is private |
| `browse` | list entries, text search, tag filter, mod filter (vanilla only, Space Age, a modset; several match any), kind filter (blueprints only by default, each kind's count, counts unchanged by the selection) and the cards' kind icons, a planner inside a book linking its book and opening its row on the entry page, a size filter leaves out unmeasured results, a book and its blueprints listed as their own results, each matching only its own text (the book's title does not list its other blueprints), with a header link to the book and opening the leaf page, a blueprint found by a community name ("red science" finds the internal name's results and the other way round), the upload date range (both ends included, one bound alone, an impossible date ignored by the page and a 400 from the API), facet counts in the filter panel (per tag as a badge inside the pill with ", N results" in its accessible name, a 0 pill muted without a badge until ticked, vanilla and Space Age, game version, factories and platforms, each leaving its own filter out and respecting the others), the game-version single-choice dropdown (choose by click or keyboard, closes on choice, Escape and an outside click, "Any version" undoes it), empty result, uploader link (guest and member) |
| `compare` | similar blueprints and compare: a leaf page's strip lists the closest production rate first (not itself, not a space platform, not an unanalysed blueprint) and links `/browse?similar_to=` (the same list, no sort, still filterable); the Compare button picks a blueprint (pressed, unpicked by a second press, kept over a reload) and opens `/compare?a=&b=` from another one, spending the pick (a fresh Compare click picks again, and the page clears a stale key on load); the metric table pairs each side's figures, is compact, and marks the larger figure (bold, "(higher)"/"(lower)" for screen readers, none for ties); swapping; the previews stacked and each metric stacked with no sideways scroll at 375px, side by side wide; unknown, private, unlisted, repeated or missing ids are 404 (also from the API, and `similar_to` of one); an owner's private blueprint has no Compare button or strip |
| `planners` | a book of deconstruction planners (settings, special filters, a blacklist's excluded entities, the planner string with no editor link), an upgrade planner's from -> to pairs with quality conditions, a planner as an entry of its own (copy, raw string, no editor), a blueprint's snap-to-grid (guest and member) |
| `entry-view` | blueprint and book pages (recipe chips show their counts; the details panel's footprint, snap-to-grid, string size and the mods a blueprint needs, an unknown mod with its names), leaf pages, no edit controls for others, private and unknown entries 404 |
| `raw-string` | raw string link on a blueprint, a book and a leaf serves the exact string as text/plain (guest and member); the live revision's link has no revision id, an older one's names it; "Open in editor" carries the same raw URL; a page on a foreign origin fetches it; no raw or editor link on a private entry |
| `revisions` | switch revisions of blueprints and books, revision list (guest and member) |
| `import` | the upload page's three tabs, "Paste string", "From file" and "Import from link" (guest and member): ARIA tablist with arrow keys and Home/End; switching keeps what each tab holds and the Upload button follows the active tab; the pastebin link is fetched and inspected by the API from the stand-in server, the same summary, copy button and tag suggestions as a pasted string show, the entry shows "Imported from"; the consent line gates the fetch (nothing is requested) and the upload; another link or a failed fetch drops the import; a site's suggested title and description fill empty fields only (Factorio Prints); unsupported, missing, blueprint-less, `0`-prefixed non-blueprint and non-https links explain themselves inline and nothing uploads; the 375 px layout; API: consent-less and look-alike hosts refused without any request, resolve answers only a blueprint with its inspection and source token, and an upload naming a `source_url` without the token of that very link and string is 422 `import_source_unverified`. Turnstile and the rate limits are off in this stack: the anonymous import check, its limits and the token's expiry are covered by the Go tests (`httpapi/upload_edit_test.go`, `import_token_test.go`, `importer/budget_test.go`) |
| `revision-notes` | an owner and an edit-link holder add a note on the new-revision form (trimmed, 280 characters, API 400 `invalid_revision_note` beyond), the revision list shows it under its revision and the first has none; a note is not on the public profile |
| `user-feed` | `/u/{name}/feed.atom` is well-formed Atom (parsed in the browser) with the publication and each revision (note as content, escaped title), only public entries (no unlisted, private or their notes), `ETag`/304 and public cache headers for a cookie-less reader, 404 for an unknown user, a deleted entry leaves it, an empty feed is valid; the profile head advertises it |
| `alias` | an alias set at upload (trimmed, lower-cased) opens the entry at `/@{user}/{alias}` with canonical `/e/{id}`; changing it on the alias page moves to the new link and the old one is a 404, clearing removes it; taken, reserved, malformed and id-shaped aliases are refused with a reason; a private entry's alias is the owner's only and belongs to the owner's name; no `/bp/{id}`; guests have no field |
| `upload` | upload blueprint/book as guest (clean link, edit link shown once and working) and member (owned), broken string; suggested tags are listed and added by a click on a pill or "Add all", never ticked unasked; guests get no visibility field, members default to public; unlisted upload opens signed out (noindex, no edit controls) but is off the profile and search; private upload 404s signed out; expiring upload: local expiry with time left on the page, on the profile card; none by default |
| `quick-preview` | `/preview` from the sidebar, unlisted read-only page, edit link once, who may publish (signed out, signed up through the edit link, owner), book refused, not in own listings or profile. Publishing a finished preview needs a render: covered by the API tests |
| `claim` | anonymous entry published to an account: create an account (passkey) from the edit-link banner, publish panel (title, private) enabled at once, then owned and listed; edit link alone offers only sign-in; no panel for visitors or on owned entries; publishing an expiring entry keeps its expiry ("Keep") |
| `visibility` | the owner changes visibility from the edit form in all six directions; a visitor then sees the entry (or 404 when private), the profile and search list it only when public; an anonymous entry's form has no visibility |
| `editing` | edit title/description and add revisions with the edit link and as owner, cancel, wrong token, revision on a slow connection keeps the token |
| `auth` | passkey sign-up (signed in at once), invalid, taken and reserved (`Ad_min`) usernames, passkey log in with no username, `?next=` return, no provider buttons without providers, a provider `?error=` explained, `/signup` with nothing pending, `/login` drops an abandoned provider sign-up, logout (ends this device's session only; the API client's session stays), a page left open after logout in another tab (next action goes to login), header rendered before scripts, a session revoked by "log out everywhere" on another device renders signed out, redirects (login, signup), sign-up on a slow connection keeps the username out of the URL, cookie-only session |
| `account` | own details, profile link; the only sign-in method cannot be removed and a second is asked for; add a passkey on this device, log in with it, remove the seeded one; an hour after the login (`ageSignIns`) a removal asks "Confirm it's you", a passkey confirms it and the removal logs the other device out; sessions list ("This device", log another session out), log out everywhere; "Export my data" downloads the JSON at once after a login and after "Confirm it's you" an hour later; "Delete account" opens at the top of its explanation (the username field not focused) and waits for the typed username (Cancel deletes nothing); deleting an hour after the login asks "Confirm it's you", then lands on `/account-deleted` signed out, the other device's token is 401, the passkey on this device is "not registered", profile and owned entry 404, the comment is a tombstone, an anonymous entry stays; a banned account that deletes itself cannot sign up again under its name, before the hard delete (`purgeDeletedAccount`) or after it (the ban hold, also for a case/`_` variant; "not available" like any taken name), while a deleted account that was not banned frees its name (`banAccount` and `purgeDeletedAccount` write the database directly). Confirming with a provider needs a provider: covered by the API tests |
| `account-recovery` | the recovery code ([api.md](api.md#sessions-and-sign-in)): shown on `/recovery-code` right after a passkey sign-up, before the page the visitor came for (copy to the clipboard, download as a text file with username and code, a reload still shows it, Continue forgets it; other tabs and visitors see "no recovery code waiting"; skippable); every account's code differs; `/recover`, linked from `/login`: a person who lost their only passkey recovers with a sloppily typed username and code (any case, spaces for dashes), is signed in as a fresh login (adds a new passkey and removes the lost one with no "Confirm it's you"), gets a new code, logs in with the new passkey, and the used code no longer works while the new one does; a code works once; a wrong, another account's, malformed, short, long or username-as-code attempt and an unknown username all read the same and use nothing up; empty fields are caught in the page; other devices stay signed in; a deleted account cannot be recovered; signed-in visitors are sent home; "Generate a new recovery code" on `/account` shows the new code once, voids the old one (the new one recovers, the old one does not) and keeps the session, and after a while asks to confirm it's you first. The provider sign-up's code and the concurrent-use race are Go tests (`httpapi/auth_recover_test.go`) |
| `my-entries` | own public, unlisted and private uploads listed with visibility, newest first, only the public one on the profile and in search; empty list |
| `favorites` | favorites list, mark/remove on entry page, leaf page, card and list; state rendered before scripts on entry, leaf, profile and collection pages |
| `collections` | list as cards, create, edit, delete, remove on collection page, add/remove and create from entry and leaf pages (written on Save; an unnamed new collection refused), picker membership from one request |
| `live-progress` | a pending leaf opens one event stream (guest and member), an open page says when its entry is deleted, a blueprint the render worker refuses shows both failures with their reason live without a reload, a refused quick preview says why and is deleted (both pause the render worker; serial) |
| `views` | a visitor's view counted once (reload and leaf page are the same visitor) and shown on the entry; a bot's visit, the owner's and an edit-link holder's are not counted; search sorts by views and cards show them (visitors use a plain Chrome user agent: HeadlessChrome is a bot) |
| `comments` | guest reads the thread (Markdown, icons) and gets a log-in link; a member posts with `#N` references (unknown numbers and script tags stay inert), edits, removes (tombstone); reports a comment; the owner removes one, then making the entry unlisted warns and deletes the thread; the edit link removes on an anonymous entry; a leaf page links to the entry's thread |
| `reports` | a guest reports an entry as someone else's work (the note is required for that reason; the flag button then shows "reported"); the anonymous uploader (edit link) gets no flag; a member reports another's entry but not their own |
| `seo` | entry description (Markdown and icons stripped), canonical, Open Graph and JSON-LD; a book leaf has its own JSON-LD (part of the entry), a single blueprint's leaf page is canonical to the entry without JSON-LD; unlisted entry noindex without JSON-LD; the OpenSearch description linked from every page; robots.txt, sitemap index and the entry sitemap listing entries (it is cached for 10 minutes, so not necessarily the one just seeded) with a cache lifetime; browse page indexed with a visible "Browse entries" h1, search results noindex with the browse canonical; the privacy policy page reached from the footer, the login page and the sign-up page |
| `spoiler` | an upload marked spoiler/NSFW shows its notice with previews hidden, "Show previews" reveals them, the details form unmarks it; the browse page lists spoilers until "Hide spoiler/NSFW entries" is ticked (`spoiler=hide`) |
| `string-tool` | `/json`: decode, edit and encode a book (the Tree tab shows the decoded book); JSON written from scratch encodes (the tree follows the field; clear empties the string field); JSON it cannot encode explained; "Open as JSON" on an entry opens its blueprint, and on a book's planner that planner, already decoded; a missing node says so |
| `challenges` | a guest finds an open challenge in the list, its goal, rules and Markdown description, and is asked to log in; the list sorts challenges into Upcoming, Open, Voting and Past, a draft and a hidden-until-start challenge are unlisted and 404 until it starts; one challenge through its timeline (upcoming: no entering, the upload page refuses; open: entering and voting; voting: no entering, votes still count; closed: results, votes, podiums, no vote buttons); entering a public blueprint through the searchable picker (books and private entries not offered, the search narrows by title), the entry page's Challenge line, making it unlisted takes it out; "Change entry" opens the picker and replaces the entry (the entered one is listed but disabled), Withdraw removes it; the "Your entry" section shows the entrant's card with its score or reasons; the upload page refuses a book, takes a 2.0 blueprint (any game version) titled after its label; votes: give and take back (none on one's own), one vote each (the others disabled until taken back), voters made before the start only (`ageAccount` then lets them), voting off (no votes, no vote podium); scores (`storeAnalysis`): a live leaderboard ranks by the goal, entries that are pending, failed, partial or rejected are public to no one else (the heading counts public entries), scores hidden until the deadline without one, staff overrides (reject, accept one the rules refuse); rules: a footprint limit, vanilla refuses Space Age content, a platform challenge refuses a factory and a factory one takes it, new revisions only (`ageRevisions`) or not, all hidden from guests; as the entrant: own score, waiting, and every rejection reason (entity count before the analysis, footprint, failed or partial analysis, staff verdict, kind, content, old revision) read in the "Your entry" section; an ended challenge's Winners and Community vote podiums (places, scores, votes, an empty step, the rest under "Other entries") |
| `public-profile` | public profile and its tabs (guest and member: entries and numbers, collections tab card with the entry count a visitor sees, to a public collection whose cards carry favorites counts; status, GitHub link (new tab), top tags, pins in order, comments tab linking to the thread), private collection and unknown user 404; the owner: sees a private entry, edits status and links (a typed URL refused), pins from a profile card, the entry page and My entries (not a private entry), arranges pins with the keyboard and unpins in the dialog |
| `leaderboards` | the sidebar's "Leaderboard", the Ships / Factories switch (`aria-current`, `?group=factories`), every board shown or in "Still filling up" with its count and what wins; ten stored ships open the fastest board (top three as cards with rank and value, places 4-10 behind "Places 4 to 10", a card opens the entry), the leader on the home page's Leaderboard records, then (the minute task) the owner's Record holder badge and held record linking its board; ten stored science builds (`storeAnalysis` `science`) open the factory boards, and once the minute task records the leader its card carries the "#1 Most science" chip (+2 for densest and cleanest) and the primary ring (computed box shadow) while the runner-up has neither, the chip on the home page, the owner's profile card and the entry page's Record row (three links to the boards); the files' tests run in order (the boards are shared) |
| `xp` | a profile's level, XP, bar and badges from an upload, someone else's favorite (one's own earns nothing) and a comment; deleting the entry takes its points and badges back |

The recovery suite has one spec, `recovery` (server/client crash and restart
while idle and during a job, server down for a while).

Not covered: previews and production analysis (no Factorio; only the recovery
suite renders), which includes the preview viewer (zoom, pan, revision
timeline, author notes: they need a rendered preview; the Go tests cover the
notes and history API, vitest the pan and zoom maths), advanced search
filters other than tags, mods, kind, upload dates and size, the icon picker, mobile layouts and browsers other
than Chromium. CI does not run the suite yet.
