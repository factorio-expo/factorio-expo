# Administration

The staff console (`apps/expo-admin/`): moderation, content and user
management, the pipeline, cleanups, challenges. One user, the owner; no roles,
no staff sign-up. The staff API is expo-api's admin listener
([api.md](api.md#staff-api-expo-api-admin-listener)); expo-admin never
touches PostgreSQL.

Request flow: browser -> identity-aware proxy (Cloudflare Access) -> reverse
proxy (admin hostname) -> expo-admin (verifies the proxy JWT) -> expo-api
admin port (shared token + `X-Staff-User`).

## Security model

The repository is public: protection comes from staff routes being
unreachable and every check being sound, never from hidden code.

- **The proxy is the gate.** Console and Dozzle are on their own hostnames
  (`ADMIN_HOST`, `ADMIN_LOGS_HOST`) behind one Access application whose Allow
  policy names the owner's email. Change the owner there, not here. Never
  widen it to a whole email provider.
- **expo-admin verifies the proxy itself** (`src/lib/access.ts`): RS256
  signature of `Cf-Access-Jwt-Assertion` against the team's keys, issuer,
  audience, expiry; staff name from the `email` claim. Without the Access
  settings every request is refused (401).
- **Dozzle has no login**; only the proxy stands in front of it, so the origin
  must not be reachable around it.
- **Separate admin listener** on an internal network, only with
  `API_ADMIN_TOKEN`; the public listener has no staff routes.
- Staff identities are not expo accounts (`users` has no staff flag); staff
  writes are recorded under the Access email. Session length is the Access
  application's (8 hours).
- CSRF origin check, strict CSP, `noindex`, no link from the public site. No
  secrets in the repository. Never container control, RCON or bridge
  commands.

## Running it

```sh
docker compose --profile admin up -d --build   # expo-admin, Dozzle, socket proxy
```

- `.env` needs `ADMIN_API_TOKEN` (`openssl rand -hex 32`); production also
  the hostnames and Access settings (`ADMIN_ACCESS_TEAM_DOMAIN`,
  `ADMIN_ACCESS_AUD`).
- **Local:** with Access unset, set `ADMIN_DEV_USER=dev` and open
  http://localhost:3200 (Dozzle http://localhost:9080). The bypass works only
  while `ADMIN_ORIGIN` is a loopback or `*.localhost` origin.
- Pages post plain HTML forms to themselves (`handlePost`: post, redirect,
  `?notice=`/`?error=`). Destructive buttons confirm in the browser
  (`data-confirm`). The console has no e2e specs; its writes are covered by Go
  tests.

## Pages (built)

- **Overview:** uploads, open reports, failed jobs, queues, render worker
  readiness (polled every 5 s), host and container CPU/memory from Dozzle.
- **Entry reports:** grouped per entry, takedowns (`stolen`) first, the
  snapshot next to the entry now. Resolve, delete the entry, ban the owner or
  a reporter (an anonymous reporter's IP hash for hours; the page never names
  a reporter). Takedown requests are just `stolen` reports.
- **Comment reports:** remove (tombstone "Removed by staff"; the audit row
  never keeps the body), resolve, ban the author.
- <a id="problem-reports"></a>**Problem reports** (the site's bug button):
  close with a status and note (deletes the contact address and IP hash), ban
  the reporter, copy as a GitHub issue (without reporter data).
- **Bans:** user bans by username (actions, reason, end or permanent; change
  end, revoke) and active IP bans by hash (lift).
- <a id="entries"></a>**Entries:** every entry whatever its state, with
  filters. An entry's page: facts, blueprints with job states and failures,
  revisions with raw strings, reports, comments. Actions: delete (soft),
  restore (until the hard-delete cleanup), re-run a phase, reindex, purge CDN
  cache.
- **Users:** accounts with search and tabs; the Deleted tab is the deletion
  queue. An account's page: clear status and links, bans, log out
  everywhere, delete account.
- <a id="failed-jobs"></a>**Failed jobs:** preview, analysis and search jobs
  failed for good; re-run one or all shown (fresh retry budget).
- <a id="tags"></a>**Tags:** groups, tags (slugs never change) and tag
  suggestion rules; the catalog reloads its rules after each change. Deleting
  a tag reindexes its entries.
- <a id="reserved-usernames"></a>**Reserved names:** normalized (lower case,
  no `_` or `-`); an account that already has one keeps it.
- <a id="home-picks"></a>**Home picks:** up to 6 blueprints for the home
  page's Staff picks, each with an optional caption; a pick whose leaf leaves
  the current revision of a listed entry is hidden.
- **Challenges:** list, editor (rules, imports, dates in UTC, options),
  publish/unpublish (not once started), entries with staff verdicts, voting
  oversight (flags, void votes; thresholds in `staffops/staffvotes.go`), and a
  test box that runs a pasted blueprint as a quick preview and checks it
  against the rules. Results publish themselves at `voting_ends_at`; XP
  awards go out once, within a minute.
- <a id="settings"></a>**Settings** (`global_settings`, defined in
  `internal/settings`; adding one needs no migration): `ALLOW_UPLOADS`,
  `ALLOW_ANONYMOUS_UPLOADS`, `ALLOW_SIGNUPS`, `ALLOW_COMMENTS`, `SITE_BANNER`.
  Only changed values are sent. API and expo-web each cache for 5 s. Paused
  actions answer 503 ([api.md](api.md#abuse-controls)); expo-web shows the
  banner and disables the affected forms.
- <a id="cleanups"></a>**Cleanups** (dry run, then an audited run):
  hard-delete deleted entries (once their search removal ran), hard-delete
  deleted accounts that own no entry, sweep expired entries, prune expired IP
  bans, re-detect mods, orphaned preview files, evict full previews to a size.
- **Audit log:** every staff write (who, when, action, target id,
  before/after, reason).

Not built yet: comment browser, user rename, search index health and
rebuild, current render job, storage and database sizes, build/config info
page, audit log filters.

**No hiding.** Staff never hide entries: a bad entry is soft-deleted and a
mistake undone with restore. Reports never hide anything automatically.
There is no single-entry hard delete; the cleanup does it.

## Account deletion

Self-service (`DELETE /api/v1/me`, fresh sign-in) or staff
(`staffops.StaffDeleteUser`); both run `accountdeletion.Service.Delete`.
Staff deletion also resolves the account's open reports and writes an audit
row; tombstones then say `admin` instead of `account`.

1. **At once:** every session and sign-in method deleted, recovery code
   cleared, profile 404, comments tombstoned (numbering kept), favorites,
   collections, status, links and pins removed, owned entries soft-deleted and
   unindexed (CDN purge when configured). Anonymous entries are untouched. The
   provider accounts may sign up again at once, unless a ban holds them.
2. **7 days later** (`DeletedAccountHold`) the daily `purge_deleted_accounts`
   task hard-deletes the entries, then the account row (bans, reports filed,
   tombstones cascade). The Cleanups page can do it sooner. The username is
   then free.
3. The id goes on the **deleted-accounts ledger** (`API_DELETED_USERS_FILE`,
   append-only, own bind mount, no personal data). **After restoring any
   database dump, run `expo-api replay-deletions`.** Never restore an older
   ledger over a newer one.

**Bans across deletion:** a deleted account with an active ban leaves
`ban_holds` (HMAC of its normalized username and provider identities) until
the ban ends (permanent: 1 year). Sign-up refuses a held username; a held
identity can neither sign up, sign in nor be linked
(`accounts.ErrIdentityHeld`). Revoking the ban does not lift holds; the hourly
sweep prunes expired ones.

**Backups** expire after at most 30 days; with the ledger replay, deleted
data never returns to use. Worst case from request to full erasure is about
60 days; the privacy policy states it.

## Monitoring and logs

Dozzle (`admin` profile, behind the same proxy) for live logs and the
Overview's current CPU/memory, reading Docker through a read-only
docker-socket-proxy. The CDN's analytics cover edge traffic. No metrics
history or disk usage yet.
