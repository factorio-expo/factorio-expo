-- Initial catalog schema. See docs/schema.md for the authoritative design
-- rationale; keep that document in sync with any future migration.

-- Every catalog key is a random 12-character id (internal/shortid, nanoid with
-- an alphanumeric alphabet). The API generates it; the database only checks
-- its shape. "C" collation keeps comparisons bytewise and case-sensitive.
CREATE DOMAIN short_id AS text COLLATE "C"
    CHECK (VALUE ~ '^[0-9A-Za-z]{12}$');

CREATE TABLE users (
    id            short_id PRIMARY KEY,
    -- Usernames are URL-safe handles (also enforced by the API). They are
    -- unique case-insensitively so "Alice" cannot impersonate "alice".
    username      text NOT NULL UNIQUE CHECK (username ~ '^[A-Za-z0-9_-]{3,32}$'),
    -- No email and no password: accounts sign in with a provider identity
    -- (auth_identities) or a passkey (user_passkeys). Signed-in devices are
    -- user_sessions rows.
    -- SHA-256 of the account's recovery code (internal/accounts/recoverycode.go):
    -- 128 random bits shown once at sign-up, which with the username signs in
    -- an account that lost every sign-in method. Used up (replaced) by the
    -- recovery it signs in; null once the account is deleted.
    recovery_code_hash bytea CHECK (length(recovery_code_hash) = 32),
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- The profile's status line ("Building a 10k SPM megabase on Gleba"):
    -- one line of plain text with icon tokens, set by the user, cleared by
    -- staff or the account's deletion. Empty for none.
    status        text NOT NULL DEFAULT ''
                      CHECK (char_length(status) <= 140 AND status !~ '[\r\n]'),
    -- Set when the account is deleted (by its owner or staff): sessions
    -- and sign-in methods deleted, profile gone, its entries soft-deleted
    -- and comments tombstoned. The row stays, holding its username, until
    -- the API's daily sweep hard-deletes it with its entries 7 days later
    -- (docs/admin.md#account-deletion).
    deleted_at    timestamptz
);

CREATE UNIQUE INDEX idx_users_username_lower ON users (lower(username));

-- Usernames nobody may sign up with (staff words, site routes, the brand).
-- Matched ignoring case, '_' and '-' ("Ad_min" is "admin"), so name holds
-- that normalized form: translate(lower(username), '_-', ''). Accounts that
-- already have such a name keep it. Rows are seeded in
-- 000002_seed_initial_data.
CREATE TABLE reserved_usernames (
    name       text PRIMARY KEY CHECK (name ~ '^[a-z0-9]{1,32}$'),
    reason     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- An account at a sign-in provider (github, discord, google, steam) linked to
-- a user. Matched by provider and the provider's own stable user id, never by
-- email or name. One account per provider per user.
CREATE TABLE auth_identities (
    provider         text NOT NULL CHECK (provider ~ '^[a-z0-9]{1,32}$'),
    provider_user_id text NOT NULL CHECK (length(provider_user_id) BETWEEN 1 AND 255),
    user_id          short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_used_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_user_id),
    UNIQUE (user_id, provider)
);

-- A WebAuthn passkey. credential is go-webauthn's Credential as JSON (public
-- key, flags, sign count, transports); credential_id is its id, unique
-- across all users, which a sign-in looks the passkey up by.
CREATE TABLE user_passkeys (
    id            short_id PRIMARY KEY,
    user_id       short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    credential_id bytea NOT NULL UNIQUE CHECK (length(credential_id) BETWEEN 1 AND 1023),
    credential    jsonb NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);

CREATE INDEX idx_user_passkeys_user ON user_passkeys (user_id);

-- A signed-in browser or API client (internal/sessions). The session JWT
-- names the row; deleting it signs that device out at once. Dates only: no
-- IP address and no user agent. last_used_at is refreshed at most every few
-- minutes, not on every request. auth_time is when the person last
-- completed a sign-in ceremony (sign-in, or "confirm it's you") on this
-- session; sensitive account changes need it to be recent. The API's hourly
-- sweep deletes expired rows.
CREATE TABLE user_sessions (
    id           short_id PRIMARY KEY,
    user_id      short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    auth_time    timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

CREATE INDEX idx_user_sessions_user ON user_sessions (user_id);
CREATE INDEX idx_user_sessions_expires ON user_sessions (expires_at);

CREATE TABLE entries (
    id                  short_id PRIMARY KEY,
    owner_user_id       short_id REFERENCES users (id),
    -- hex SHA-256 of the anonymous edit token; the token itself is never stored.
    edit_token_hash     text UNIQUE,
    -- public: listed and searchable. unlisted: anyone with the link can view
    -- it, but no listing or search shows it. private: only its owner.
    visibility          text NOT NULL DEFAULT 'public'
                            CHECK (visibility IN ('public', 'unlisted', 'private')),
    current_revision_id short_id, -- FK added after revisions exists (circular).
    title               text NOT NULL DEFAULT '',
    description         text NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    -- Soft delete: a deleted entry is never hard-removed so the search
    -- projection sync (entry_search_jobs below) can still see it long enough
    -- to issue a Typesense delete, and so cascading revisions/books/
    -- blueprints/previews remain intact for audit/undo. Every entry read
    -- path filters on entry_live (below).
    deleted_at timestamptz,
    -- When the entry ends: it reads as missing once expired, and the API's
    -- minute sweep removes it after that (soft delete plus a search delete for
    -- an upload, hard delete for a quick preview). Null means it lives until
    -- deleted. Uploads may choose one; every quick preview has one.
    expires_at timestamptz,
    -- A quick preview: never listed or indexed, read-only, hard-deleted when
    -- its render or analysis fails for good. Publishing clears it.
    quick_preview boolean NOT NULL DEFAULT false,
    -- Spoiler/NSFW: set by whoever may edit the entry; previews show blurred
    -- until a click, and the browse page leaves such entries out by default.
    spoiler boolean NOT NULL DEFAULT false,
    -- The last comment number handed out (comments.number). Never reused, so
    -- a #N link never points at a different comment after a purge.
    comment_seq integer NOT NULL DEFAULT 0,
    -- Where an imported blueprint was fetched from (an allowlisted https
    -- link, internal/importer); null for an upload. Shown as "Imported from".
    source_url text CHECK (source_url IS NULL OR (source_url ~ '^https://' AND char_length(source_url) <= 2000)),
    CHECK (visibility = 'public' OR owner_user_id IS NOT NULL),
    CHECK (NOT quick_preview OR expires_at IS NOT NULL)
);

-- The entry lifecycle rules, one place each (Go twin: catalog/lifecycle.go;
-- lifecycle_test checks they agree). Queries filter on them instead of
-- spelling the columns out. Plain SQL functions, so the planner inlines them
-- and indexes still apply.
--
-- entry_live: not deleted and not expired. Anything else reads as missing.
CREATE FUNCTION entry_live(entry entries) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT entry.deleted_at IS NULL AND (entry.expires_at IS NULL OR entry.expires_at > now()) $$;

-- entry_visible_to: whether viewer (null when signed out) may see the entry:
-- private entries only their owner. Says nothing about liveness.
CREATE FUNCTION entry_visible_to(entry entries, viewer text) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT entry.visibility <> 'private' OR coalesce(entry.owner_user_id = viewer, false) $$;

-- entry_listed: live, public, not a quick preview (those are stored public
-- but are never listed) and with a revision. Listed entries are the ones in
-- search, on public profiles, and the only ones that take comments.
CREATE FUNCTION entry_listed(entry entries) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT entry.visibility = 'public' AND NOT entry.quick_preview
              AND entry.current_revision_id IS NOT NULL AND entry_live(entry) $$;

-- entry_lasting: live and not a quick preview (an upload, or a published
-- one). Only lasting entries get cards and stay in favorites, collections and
-- their owner's listings.
CREATE FUNCTION entry_lasting(entry entries) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT NOT entry.quick_preview AND entry_live(entry) $$;

CREATE TABLE revisions (
    id              short_id PRIMARY KEY,
    entry_id        short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    version_number  integer NOT NULL,
    original_string text NOT NULL,
    tree            jsonb NOT NULL,
    -- The game version that exported the submission, read out of the exchange
    -- string itself (e.g. "2.0.15"); empty when the string carries none. It
    -- says nothing about which mods were active.
    game_version    text NOT NULL DEFAULT '',
    -- Factorio 2.1 compatibility of a submission made before 2.1
    -- (blueprint.Compat21): empty when made in 2.1 or the version is unknown.
    compat_21       text NOT NULL DEFAULT ''
                        CHECK (compat_21 IN ('', 'compatible', 'incompatible')),
    -- The uploader's optional change note (at most 280 characters, trimmed;
    -- empty for none): shown in the revision timeline and the user's feed.
    note            text NOT NULL DEFAULT '' CHECK (char_length(note) <= 280),
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entry_id, version_number)
);

-- An entry's vanity alias, served at /@{username}/{slug}. At most one per
-- entry, set by its signed-in owner (an anonymous entry has none), unique per
-- user. user_id is the owner when it was set, kept here only for that
-- uniqueness; an alias whose entry is no longer live is replaced by the next
-- user to claim the slug (catalog/alias.go), so deleting an entry needs no
-- cleanup. The slug rules are twinned in catalog/alias.go.
CREATE TABLE entry_aliases (
    entry_id short_id PRIMARY KEY REFERENCES entries (id) ON DELETE CASCADE,
    user_id  short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    slug     text NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,58}[a-z0-9]$'),
    UNIQUE (user_id, slug)
);

ALTER TABLE entries
    ADD CONSTRAINT entries_current_revision_id_fkey
    FOREIGN KEY (current_revision_id) REFERENCES revisions (id);

CREATE TABLE books (
    id           short_id PRIMARY KEY,
    revision_id  short_id NOT NULL REFERENCES revisions (id) ON DELETE CASCADE,
    label        text NOT NULL DEFAULT '',
    description  text NOT NULL DEFAULT '',
    icons        text NOT NULL DEFAULT '',
    active_index integer
);

CREATE TABLE blueprints (
    id              short_id PRIMARY KEY,
    revision_id     short_id NOT NULL REFERENCES revisions (id) ON DELETE CASCADE,
    label           text NOT NULL DEFAULT '',
    description     text NOT NULL DEFAULT '',
    icons           text NOT NULL DEFAULT '',
    exchange_string text NOT NULL,
    -- SHA-256 of exchange_string: finds leaves with the same string, whose
    -- stored preview, analysis or final failure a new leaf reuses instead of
    -- rendering again (catalog/reuse.go). Matches are confirmed on the string.
    exchange_sha256 bytea NOT NULL CHECK (length(exchange_sha256) = 32),
    entity_count    integer NOT NULL DEFAULT 0,
    -- Leaf ordinal in tree (depth-first) order within the revision; the cover
    -- image is the lowest-position leaf that has a preview.
    position        integer NOT NULL DEFAULT 0,
    -- The blueprint's snap-to-grid setting (blueprint.SnapToGrid: width,
    -- height, absolute, offset_x, offset_y); null when it has none.
    snap_to_grid    jsonb CHECK (snap_to_grid IS NULL OR jsonb_typeof(snap_to_grid) = 'object'),
    -- Factorio 2.1 compatibility of this leaf (blueprint.Compat21): empty when
    -- made in 2.1 or the version is unknown.
    compat_21       text NOT NULL DEFAULT ''
                        CHECK (compat_21 IN ('', 'compatible', 'incompatible'))
);

-- Upgrade and deconstruction planners: tree leaves that are listed and
-- copied, never rendered or analysed (no jobs, no stats). settings is the
-- planner's configuration in the stored shape (blueprint.UpgradeSettings or
-- blueprint.DeconstructionSettings, by kind).
CREATE TABLE planners (
    id              short_id PRIMARY KEY,
    revision_id     short_id NOT NULL REFERENCES revisions (id) ON DELETE CASCADE,
    kind            text NOT NULL CHECK (kind IN ('upgrade_planner', 'deconstruction_planner')),
    label           text NOT NULL DEFAULT '',
    description     text NOT NULL DEFAULT '',
    icons           text NOT NULL DEFAULT '',
    exchange_string text NOT NULL,
    settings        jsonb NOT NULL CHECK (jsonb_typeof(settings) = 'object')
);

CREATE INDEX idx_books_revision_id ON books (revision_id);
CREATE INDEX idx_blueprints_revision_id ON blueprints (revision_id);
CREATE INDEX idx_blueprints_exchange_sha256 ON blueprints (exchange_sha256);
CREATE INDEX idx_planners_revision_id ON planners (revision_id);

-- Written in the same transaction as its revision, so its creation time is
-- revisions.created_at (likewise for books and blueprints).
CREATE TABLE blueprint_stats (
    blueprint_id short_id PRIMARY KEY REFERENCES blueprints (id) ON DELETE CASCADE,
    entities     jsonb NOT NULL DEFAULT '[]'
                     CHECK (jsonb_typeof(entities) = 'array'),
    recipes      jsonb NOT NULL DEFAULT '[]'
                     CHECK (jsonb_typeof(recipes) = 'array'),
    -- Mod detection from the entity and recipe names (internal/mods): a
    -- mods.Detection, made against the embedded name table mods_version
    -- names. The API re-detects rows of an older version at startup.
    mods         jsonb NOT NULL DEFAULT '{}'
                     CHECK (jsonb_typeof(mods) = 'object'),
    mods_version text NOT NULL DEFAULT '',
    -- Structural fields above are independent of the production analysis
    -- fields below: a production refresh never overwrites them, and vice
    -- versa.
    production                 jsonb NOT NULL DEFAULT '{}',
    production_schema_version  integer
                     CHECK (production_schema_version IS NULL OR production_schema_version > 0),
    -- Named "state", not "quality", to avoid confusion with Factorio's
    -- quality game mechanic: it reflects unresolved production materials
    -- only, not an item/entity quality tier.
    production_state            text
                     CHECK (production_state IS NULL OR production_state IN ('complete', 'partial')),
    production_stale            boolean NOT NULL DEFAULT false,
    production_calculated_at    timestamptz
);

-- A preview row only exists once a render has succeeded and both its files
-- are in the API's file storage; lifecycle state lives in the API-owned
-- phase_jobs table. source_sha256 (of the runner's PNG) names both files
-- (`preview.Files`: lossless WebP and a lossy WebP thumbnail) and is their
-- ETag, so identical renders share files. Media type (always WebP), file sizes
-- and thumbnail dimensions are derived, not stored.
CREATE TABLE previews (
    blueprint_id  short_id PRIMARY KEY REFERENCES blueprints (id) ON DELETE CASCADE,
    source_sha256 text NOT NULL CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
    width         integer NOT NULL CHECK (width > 0),
    height        integer NOT NULL CHECK (height > 0),
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_previews_source ON previews (source_sha256);

-- Preview files that may have lost their last reference. Every deleted
-- previews row lands here (the trigger also catches cascades from hard-deleted
-- entries), as does an import whose row was not inserted. The minute sweep
-- deletes the files of a source no previews row references, then the row.
CREATE TABLE preview_file_deletions (
    source_sha256 text PRIMARY KEY,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION queue_preview_file_deletion() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO preview_file_deletions (source_sha256)
    VALUES (OLD.source_sha256)
    ON CONFLICT (source_sha256) DO NOTHING;
    RETURN OLD;
END;
$$;

CREATE TRIGGER previews_queue_file_deletion
AFTER DELETE ON previews
FOR EACH ROW EXECUTE FUNCTION queue_preview_file_deletion();

-- A re-render that produced a different image (a newer pinned game) points
-- the row at new files; the old ones may have lost their last reference.
CREATE TRIGGER previews_queue_replaced_file_deletion
AFTER UPDATE OF source_sha256 ON previews
FOR EACH ROW WHEN (OLD.source_sha256 IS DISTINCT FROM NEW.source_sha256)
EXECUTE FUNCTION queue_preview_file_deletion();

-- The stored files of one source, for the storage budget
-- (docs/architecture.md#storage-budget). Written by preview.Files whenever it
-- writes or finds a source's files, deleted with them. full_bytes is null when
-- there is no full file: a small preview (thumbnail only) or one the budget
-- sweep evicted. stored_at is when the files were last written, so a
-- re-rendered full file counts as new; the sweep evicts the oldest first.
CREATE TABLE preview_files (
    source_sha256   text PRIMARY KEY CHECK (source_sha256 ~ '^[0-9a-f]{64}$'),
    thumbnail_bytes bigint NOT NULL CHECK (thumbnail_bytes >= 0),
    full_bytes      bigint CHECK (full_bytes >= 0),
    stored_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_preview_files_evictable ON preview_files (stored_at)
    WHERE full_bytes IS NOT NULL;

-- Durable API dispatch intent and runner-job pairing, one row per leaf and
-- phase ('preview', 'analysis'). The runner still owns execution, leases, and
-- attempts; this row lets the API retry submission and reconcile missing
-- work. A preview row is removed once a `previews` row exists (and added again
-- to re-render a full preview the storage budget evicted); an analysis row once
-- a current production snapshot is stored in blueprint_stats.
CREATE TABLE phase_jobs (
    blueprint_id   short_id NOT NULL REFERENCES blueprints (id) ON DELETE CASCADE,
    phase          text NOT NULL CHECK (phase IN ('preview', 'analysis')),
    runner_job_id  text,
    state          text NOT NULL DEFAULT 'pending'
                       CHECK (state IN ('pending', 'failed')),
    retry_count    integer NOT NULL DEFAULT 0,
    available_at   timestamptz NOT NULL DEFAULT now(),
    failure_code   text NOT NULL DEFAULT '',
    failure_detail text NOT NULL DEFAULT '',
    -- Render queue (internal/renderqueue): who the work belongs to
    -- ('u:<user id>', 'ip:<hex HMAC of the address>', 'staff', 'system'; the
    -- per-uploader cap counts by it) and its place: queued time plus the
    -- delay of its kind. The dispatcher sends the lowest order_at, spaced per
    -- queue_key, while the runner holds fewer than the window. Kept only while
    -- the row exists.
    queue_key      text NOT NULL DEFAULT 'system',
    order_at       timestamptz NOT NULL DEFAULT now(),
    -- The runner job finished (succeeded or failed) while its result is still
    -- being imported: it no longer counts against the dispatch window.
    runner_settled boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (blueprint_id, phase)
);

CREATE INDEX idx_phase_jobs_due ON phase_jobs (state, available_at);
CREATE INDEX idx_phase_jobs_queue_key ON phase_jobs (queue_key) WHERE state = 'pending';
CREATE INDEX idx_phase_jobs_runner_job ON phase_jobs (runner_job_id) WHERE runner_job_id IS NOT NULL;

-- The stored facts behind each leaf's preview and analysis states, one row per
-- blueprint; progress.LeafOf turns them into states (an open job is pending,
-- else a stored result is ready, else a job failed for good is failed).
CREATE VIEW leaf_progress AS
SELECT
    b.id AS blueprint_id,
    b.revision_id,
    b.position,
    (p.blueprint_id IS NOT NULL)::boolean AS has_preview,
    COALESCE(pj.state, '')::text AS preview_job_state,
    COALESCE(pj.failure_code, '')::text AS preview_failure_code,
    (bs.production_schema_version IS NOT NULL AND bs.production_state IS NOT NULL)::boolean AS has_production,
    COALESCE(aj.state, '')::text AS analysis_job_state,
    COALESCE(aj.failure_code, '')::text AS analysis_failure_code
FROM blueprints b
LEFT JOIN previews p ON p.blueprint_id = b.id
LEFT JOIN phase_jobs pj ON pj.blueprint_id = b.id AND pj.phase = 'preview'
LEFT JOIN blueprint_stats bs ON bs.blueprint_id = b.id
LEFT JOIN phase_jobs aj ON aj.blueprint_id = b.id AND aj.phase = 'analysis';

-- Durable API dispatch intent for syncing one entry into the Typesense
-- search projection. Mirrors phase_jobs: the API
-- enqueues a row whenever an entry's searchable facts change (creation, new
-- revision, tag changes, preview/analysis import, soft delete), and a
-- background dispatcher (internal/search) reconciles it against the search
-- client. `action` distinguishes an upsert from a removal so a soft-deleted
-- entry can still be pulled out of the index. The row is removed once the
-- sync succeeds, so presence means "out of date with the index".
CREATE TABLE entry_search_jobs
(
    entry_id       short_id PRIMARY KEY REFERENCES entries (id) ON DELETE CASCADE,
    action         text        NOT NULL DEFAULT 'index'
        CHECK (action IN ('index', 'delete')),
    state          text        NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'failed')),
    retry_count    integer     NOT NULL DEFAULT 0,
    available_at   timestamptz NOT NULL DEFAULT now(),
    failure_code   text        NOT NULL DEFAULT '',
    failure_detail text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_entry_search_jobs_due ON entry_search_jobs (state, available_at);

-- Tags are a curated, admin-defined taxonomy (no user-created folksonomy
-- tags in v1); the rows are seeded in 000002_seed_initial_data.
CREATE TABLE tag_groups (
    id           short_id PRIMARY KEY,
    slug         text NOT NULL UNIQUE,
    display_name text NOT NULL,
    position     integer NOT NULL
);

CREATE TABLE tags (
    id           short_id PRIMARY KEY,
    group_id     short_id NOT NULL REFERENCES tag_groups (id),
    slug         text NOT NULL UNIQUE,
    display_name text NOT NULL,
    -- A rich-text icon token such as "[item=splitter]", empty for none.
    icon         text NOT NULL DEFAULT ''
        CONSTRAINT tags_icon_check CHECK (icon = '' OR icon ~ '^\[[a-z-]+=[A-Za-z0-9_-]+\]$'),
    position     integer NOT NULL
);

CREATE INDEX idx_tags_group ON tags (group_id, position);

CREATE TABLE entry_tags (
    entry_id short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    tag_id   short_id NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, tag_id)
);

-- Tag suggestion rules: names in an exchange string that point at a tag. A
-- rule matches when the submission (all leaves together, every quality) has
-- at least `min_count` (and, when set, at most `max_count`) entities of that
-- name, entities set to that recipe, or (kind 'quality', the name a quality) entities of that quality, and then adds `points` to its tag (catalog.suggestTags). Only facts the
-- string proves get rules, so layout, design and purpose tags have none.
-- Names are prototype names of the pinned game (base and Space Age); the
-- rows are seeded in 000002_seed_initial_data.
CREATE TABLE tag_suggestion_rules (
    tag_id    short_id NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    kind      text NOT NULL CHECK (kind IN ('entity', 'recipe', 'quality')),
    name      text NOT NULL,
    min_count integer NOT NULL CHECK (min_count >= 1),
    max_count integer CHECK (max_count >= min_count),
    points    integer NOT NULL DEFAULT 1 CHECK (points >= 1),
    PRIMARY KEY (tag_id, kind, name)
);

CREATE TABLE favorites (
    user_id    short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    entry_id   short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, entry_id)
);

CREATE TABLE collections (
    id          short_id PRIMARY KEY,
    user_id     short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    visibility  text NOT NULL DEFAULT 'private'
                    CHECK (visibility IN ('public', 'private')),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE collection_entries (
    collection_id short_id NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    entry_id      short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    position      integer NOT NULL DEFAULT 0,
    added_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (collection_id, entry_id)
);

-- All-time page views per entry. The API counts views in memory (one per
-- visitor per entry per day) and adds them here about once a minute; only
-- live, non-private entries that are not quick previews get counted. A
-- separate table keeps these frequent updates off the entries rows.
-- indexed_views is the count last written to the search index: rows where it
-- differs from views are the ones the periodic search refresh still has to send.
CREATE TABLE entry_view_counts (
    entry_id      short_id PRIMARY KEY REFERENCES entries (id) ON DELETE CASCADE,
    views         bigint NOT NULL DEFAULT 0 CHECK (views >= 0),
    indexed_views bigint NOT NULL DEFAULT 0
);

CREATE INDEX idx_entry_view_counts_unindexed ON entry_view_counts (entry_id)
    WHERE views <> indexed_views;

-- Page views per entry per UTC day, written with the same flush as the
-- all-time count. Trending reads the sum since the start of today, this
-- week (Monday) or this month; days older than a month are pruned.
CREATE TABLE entry_view_days (
    day      date NOT NULL,
    entry_id short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    views    bigint NOT NULL CHECK (views > 0),
    PRIMARY KEY (day, entry_id)
);

-- A profile's social links: the user's name on a site from a fixed list, one
-- per site. Only the name is stored; the link is built from it
-- (catalog/profile.go), so a profile never links anywhere else. Deleted with
-- the account's profile at its deletion.
CREATE TABLE user_social_links (
    user_id  short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    platform text NOT NULL
                 CHECK (platform IN ('youtube', 'twitch', 'github', 'reddit', 'bluesky', 'steam', 'x')),
    handle   text NOT NULL CHECK (char_length(handle) BETWEEN 1 AND 253),
    PRIMARY KEY (user_id, platform)
);

-- Entries a user pinned to the top of their profile, in their order (at most
-- six, enforced by the API). Only the user's own listed entries can be
-- pinned; one that stops being listed (deleted, expired, made unlisted or
-- private) simply drops out of the profile, without a write, and its row is
-- pruned at the user's next pin change.
CREATE TABLE user_pinned_entries (
    user_id   short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    entry_id  short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    position  integer NOT NULL,
    pinned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, entry_id)
);

CREATE INDEX idx_user_pinned_entries_entry ON user_pinned_entries (entry_id);

CREATE INDEX idx_favorites_entry_id ON favorites (entry_id);
CREATE INDEX idx_collection_entries_entry_id ON collection_entries (entry_id);
CREATE INDEX idx_entries_owner_user_id ON entries (owner_user_id);
CREATE INDEX idx_entries_expires_at ON entries (expires_at) WHERE expires_at IS NOT NULL;

-- One flat comment thread per entry, shared by every leaf and revision. Only
-- live public entries that are not quick previews take comments; when an
-- entry stops being public its comments (and their reports) are deleted.
-- number is the public handle (#N), unique per entry and taken from
-- entries.comment_seq. revision_number is the entry's live revision when the
-- comment was posted. A removed comment stays as a tombstone (body wiped) so
-- numbering and #N references keep their meaning.
CREATE TABLE comments (
    id              short_id PRIMARY KEY,
    entry_id        short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    number          integer NOT NULL CHECK (number > 0),
    author_user_id  short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    revision_number integer NOT NULL,
    body            text NOT NULL CHECK (char_length(body) <= 4000),
    created_at      timestamptz NOT NULL DEFAULT now(),
    edited_at       timestamptz,
    deleted_at      timestamptz,
    -- 'account': the author deleted their account.
    deleted_by      text CHECK (deleted_by IN ('author', 'owner', 'admin', 'account')),
    UNIQUE (entry_id, number),
    CHECK ((deleted_at IS NULL) = (deleted_by IS NULL)),
    CHECK (CASE WHEN deleted_at IS NULL THEN body <> '' ELSE body = '' END)
);

CREATE INDEX idx_comments_author ON comments (author_user_id, created_at);

-- One report per user per comment, waiting for the admin panel. body_snapshot
-- is the text when reported: comments can be edited any time.
CREATE TABLE comment_reports (
    comment_id       short_id NOT NULL REFERENCES comments (id) ON DELETE CASCADE,
    reporter_user_id short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason           text NOT NULL CHECK (reason IN ('spam', 'abuse', 'off_topic', 'other')),
    note             text NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    body_snapshot    text NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    resolved_at      timestamptz,
    -- The staff member's console username.
    resolved_by      text,
    PRIMARY KEY (comment_id, reporter_user_id)
);

CREATE INDEX idx_comment_reports_open ON comment_reports (created_at) WHERE resolved_at IS NULL;

-- Notes the entry's editor pins on a leaf's preview. x and y are fractions of
-- the preview image (0 = left/top, 1 = right/bottom), so a pin follows the
-- image at any size. A note belongs to one leaf, hence one revision: a new
-- revision starts with none. The pin number shown is the note's rank by
-- (created_at, id), so deleting one renumbers the rest. Notes are public
-- wherever the leaf is; body is comment-style Markdown.
CREATE TABLE blueprint_notes (
    id           short_id PRIMARY KEY,
    blueprint_id short_id NOT NULL REFERENCES blueprints (id) ON DELETE CASCADE,
    x            double precision NOT NULL CHECK (x >= 0 AND x <= 1),
    y            double precision NOT NULL CHECK (y >= 0 AND y <= 1),
    body         text NOT NULL CHECK (body <> '' AND char_length(body) <= 500),
    created_at   timestamptz NOT NULL DEFAULT now(),
    edited_at    timestamptz
);

CREATE INDEX idx_blueprint_notes_blueprint ON blueprint_notes (blueprint_id, created_at, id);

-- Reports of an entry (offensive, someone else's work, ...), waiting for the
-- admin panel. Anyone who can see the entry may report it, signed in or not,
-- so a creator without an account can claim their work. A signed-in report
-- names its reporter; an anonymous one keeps only the keyed hash of the
-- reporter's address (as ip_bans do), and only while the report is open: it
-- deduplicates and lets an admin ban a report flood. Each reporter has at most
-- one open report per entry. The snapshots are the entry as reported: its
-- title and description can be edited and its live revision replaced.
-- Reports never hide an entry by themselves; an admin decides.
CREATE TABLE entry_reports (
    id                   short_id PRIMARY KEY,
    entry_id             short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    reporter_user_id     short_id REFERENCES users (id) ON DELETE CASCADE,
    reporter_ip_hash     bytea CHECK (length(reporter_ip_hash) = 32),
    reason               text NOT NULL
                             CHECK (reason IN ('stolen', 'offensive', 'spam', 'misleading', 'other')),
    note                 text NOT NULL DEFAULT '' CHECK (char_length(note) <= 1000),
    revision_number      integer NOT NULL,
    title_snapshot       text NOT NULL,
    description_snapshot text NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    resolved_at          timestamptz,
    -- The staff member's console username.
    resolved_by          text,
    CHECK (reporter_user_id IS NULL OR reporter_ip_hash IS NULL),
    CHECK (resolved_at IS NULL OR reporter_ip_hash IS NULL)
);

CREATE UNIQUE INDEX idx_entry_reports_open_user ON entry_reports (entry_id, reporter_user_id)
    WHERE resolved_at IS NULL AND reporter_user_id IS NOT NULL;
CREATE UNIQUE INDEX idx_entry_reports_open_ip ON entry_reports (entry_id, reporter_ip_hash)
    WHERE resolved_at IS NULL AND reporter_ip_hash IS NOT NULL;
CREATE INDEX idx_entry_reports_open ON entry_reports (created_at) WHERE resolved_at IS NULL;

-- Problem reports: the site's bug button (a broken page, a wrong render or
-- analysis), from anyone, with the page's context. Staff review them in the
-- console. The reporter is an account or, signed out, an IP hash (as
-- entry_reports; cleared when resolved). email is an opt-in contact address
-- for this report only: cleared when it is resolved, or 30 days after it was
-- sent while still open. A resolved report is deleted 7 days later (the
-- API's hourly sweep).
CREATE TABLE problem_reports (
    id               short_id PRIMARY KEY,
    reporter_user_id short_id REFERENCES users (id) ON DELETE CASCADE,
    reporter_ip_hash bytea CHECK (length(reporter_ip_hash) = 32),
    body             text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    email            text CHECK (char_length(email) BETWEEN 3 AND 254),
    -- The page's path, never its query (it may hold an edit token).
    page_path        text NOT NULL DEFAULT '' CHECK (char_length(page_path) <= 500),
    -- The entry and blueprint the page showed; no foreign keys, so the
    -- report outlives them.
    entry_id         short_id,
    blueprint_id     short_id,
    -- What the page knew (game version, preview and analysis states, viewport):
    -- short strings by short keys.
    context          jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(context) = 'object'),
    user_agent       text NOT NULL DEFAULT '' CHECK (char_length(user_agent) <= 500),
    created_at       timestamptz NOT NULL DEFAULT now(),
    status           text NOT NULL DEFAULT 'open'
                         CHECK (status IN ('open', 'fixed', 'wont_fix', 'duplicate', 'spam')),
    -- Staff only: what was done about it.
    staff_note       text NOT NULL DEFAULT '' CHECK (char_length(staff_note) <= 1000),
    resolved_at      timestamptz,
    -- The staff member's console username.
    resolved_by      text,
    CHECK (reporter_user_id IS NULL OR reporter_ip_hash IS NULL),
    CHECK ((status = 'open') = (resolved_at IS NULL)),
    CHECK (resolved_at IS NULL OR (reporter_ip_hash IS NULL AND email IS NULL))
);

CREATE INDEX idx_problem_reports_open ON problem_reports (created_at) WHERE resolved_at IS NULL;
CREATE INDEX idx_problem_reports_resolved ON problem_reports (resolved_at) WHERE resolved_at IS NOT NULL;

-- Bans are generic: each names the actions it blocks. A ban ends at
-- expires_at (null: permanent) or when revoked; rows are kept as history.
-- banned_by/revoked_by are the staff member's console username (users has no staff flag).
CREATE TABLE user_bans (
    id          short_id PRIMARY KEY,
    user_id     short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    actions     text[] NOT NULL
                    CHECK (cardinality(actions) > 0
                        AND actions <@ ARRAY['comment', 'upload', 'report']::text[]),
    reason_code text NOT NULL CHECK (reason_code IN ('spam', 'abuse', 'illegal_content', 'other')),
    note        text NOT NULL DEFAULT '',
    banned_by   text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    revoked_at  timestamptz,
    revoked_by  text
);

CREATE INDEX idx_user_bans_user ON user_bans (user_id);

-- ban_active: neither revoked nor expired. Go twin: catalog.BanActive
-- (lifecycle_test checks they agree); queries filter on it.
CREATE FUNCTION ban_active(ban user_bans) RETURNS boolean
    LANGUAGE sql STABLE
    AS $$ SELECT ban.revoked_at IS NULL AND (ban.expires_at IS NULL OR ban.expires_at > now()) $$;

-- Short address bans, kept apart from user bans: addresses rotate, so one
-- lasts hours (API_IP_BAN_DURATION) while the user ban may last days. One row
-- per address and action; a repeat extends it. ip_hash is an HMAC-SHA256 of
-- the client address (IPv6 by /64) under API_IP_HASH_SECRET, never the
-- address. Only banned addresses are stored: a user ban adds the address the
-- banned account next tries a blocked action from, for each action it bans.
-- An address ban blocks the action for anonymous callers and every account
-- from that address. Expired rows are pruned hourly.
CREATE TABLE ip_bans (
    ip_hash     bytea NOT NULL CHECK (length(ip_hash) = 32),
    action      text NOT NULL CHECK (action IN ('comment', 'upload', 'report')),
    reason_code text NOT NULL CHECK (reason_code IN ('spam', 'abuse', 'illegal_content', 'other')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    PRIMARY KEY (ip_hash, action)
);

CREATE INDEX idx_ip_bans_expires ON ip_bans (expires_at);

-- Ban holds: what a deleted account with an active ban leaves behind, so
-- deleting the account does not lift the ban (user_bans rows go with the
-- account). hash is an HMAC-SHA256 (internal/banhold, keyed from
-- API_IP_HASH_SECRET) of its username, normalized like reserved_usernames
-- (kind 'username'), or of one of its provider identities, provider and
-- provider user id (kind 'identity'); never the name or id itself. Written at
-- the account's soft delete, kept until the ban ends (a permanent ban: one
-- year). A sign-up with a held username or identity, and linking a held
-- identity, are refused. Expired rows are pruned hourly.
CREATE TABLE ban_holds (
    hash       bytea PRIMARY KEY CHECK (length(hash) = 32),
    kind       text NOT NULL CHECK (kind IN ('username', 'identity')),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX idx_ban_holds_expires ON ban_holds (expires_at);

-- Design challenges: official, made in the
-- staff console. rules is internal/challenge's Rules (environment, goal,
-- restrictions); the API checks each submission against it when read, from
-- stored facts, so a submission has no stored score. Status comes from the
-- times: draft (published_at null), upcoming, open (submissions), judging
-- (votes only), closed.
CREATE TABLE challenges (
    id                    short_id PRIMARY KEY,
    -- The challenge's public name in URLs.
    slug                  text NOT NULL UNIQUE
                              CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND char_length(slug) <= 80),
    title                 text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 120),
    -- One line for cards and lists.
    summary               text NOT NULL DEFAULT '' CHECK (char_length(summary) <= 300),
    -- Markdown: the story, the criteria in words, examples.
    description           text NOT NULL DEFAULT '' CHECK (char_length(description) <= 10000),
    -- Signal icon tokens, like a blueprint's.
    icons                 text NOT NULL DEFAULT '' CHECK (char_length(icons) <= 500),
    rules                 jsonb NOT NULL CHECK (jsonb_typeof(rules) = 'object'),
    starts_at             timestamptz NOT NULL,
    ends_at               timestamptz NOT NULL,
    voting_ends_at        timestamptz NOT NULL,
    -- The scored revision must be made after starts_at.
    require_new_revisions boolean NOT NULL DEFAULT true,
    -- Only accounts made before starts_at may vote.
    voters_before_start   boolean NOT NULL DEFAULT true,
    voting_enabled        boolean NOT NULL DEFAULT true,
    votes_per_user        integer NOT NULL DEFAULT 3 CHECK (votes_per_user BETWEEN 1 AND 20),
    -- Scores show while the challenge is open; otherwise only once it ends.
    live_leaderboard      boolean NOT NULL DEFAULT true,
    -- Not listed and not found until starts_at (an advent calendar).
    hidden_until_start    boolean NOT NULL DEFAULT false,
    published_at          timestamptz,
    -- When the closed challenge's XP awards were handed out (the
    -- challenge_awards task, catalog/xpawards.go): once, after voting_ends_at.
    awarded_at            timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CHECK (starts_at < ends_at AND ends_at <= voting_ends_at)
);

-- An entry submitted to a challenge by its owner. While the challenge is open
-- the entry's current revision is scored; afterwards its newest revision made
-- before ends_at. A user has one active submission per challenge (a new one
-- withdraws the old), and an entry is in a challenge once. An entry that
-- stops being listed while the challenge is open is withdrawn for good
-- (the trigger below); going public again does not bring it back.
CREATE TABLE challenge_submissions (
    id               short_id PRIMARY KEY,
    challenge_id     short_id NOT NULL REFERENCES challenges (id) ON DELETE CASCADE,
    entry_id         short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    user_id          short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    submitted_at     timestamptz NOT NULL DEFAULT now(),
    withdrawn_at     timestamptz,
    -- replaced: the user submitted another entry; user: withdrawn by the
    -- user; unlisted: the entry stopped being listed.
    withdrawn_reason text CHECK (withdrawn_reason IN ('replaced', 'user', 'unlisted')),
    -- A staff override of the rule check, with its reason.
    staff_verdict    text CHECK (staff_verdict IN ('accepted', 'rejected')),
    staff_reason     text NOT NULL DEFAULT '' CHECK (char_length(staff_reason) <= 500),
    CHECK ((withdrawn_at IS NULL) = (withdrawn_reason IS NULL))
);

CREATE UNIQUE INDEX idx_challenge_submissions_user ON challenge_submissions (challenge_id, user_id)
    WHERE withdrawn_at IS NULL;
CREATE UNIQUE INDEX idx_challenge_submissions_entry ON challenge_submissions (challenge_id, entry_id)
    WHERE withdrawn_at IS NULL;
CREATE INDEX idx_challenge_submissions_entry_id ON challenge_submissions (entry_id);

CREATE FUNCTION withdraw_unlisted_challenge_entry() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    UPDATE challenge_submissions s
    SET withdrawn_at = now(), withdrawn_reason = 'unlisted'
    FROM challenges c
    WHERE s.entry_id = NEW.id AND s.withdrawn_at IS NULL
      AND c.id = s.challenge_id AND c.ends_at > now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER entries_withdraw_challenge_entry
AFTER UPDATE ON entries
-- Not "was listed": an expired entry is no longer listed by the time the
-- sweep soft-deletes it.
FOR EACH ROW WHEN (NOT entry_listed(NEW))
EXECUTE FUNCTION withdraw_unlisted_challenge_entry();

-- Community votes: one per user and entry, votes_per_user per challenge
-- (checked by the API under the voter's row lock), never on the voter's own
-- entry. A vote can be taken back until voting_ends_at. Counts stay hidden
-- until then; only votes on active submissions count. Staff void a suspect
-- vote (voided_at, with a reason): it stops counting but keeps its row, so the
-- voter cannot take it back and give it again, and it still uses their slot.
CREATE TABLE challenge_votes (
    challenge_id short_id NOT NULL REFERENCES challenges (id) ON DELETE CASCADE,
    user_id      short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    entry_id     short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    voided_at    timestamptz,
    void_reason  text NOT NULL DEFAULT '' CHECK (char_length(void_reason) <= 500),
    PRIMARY KEY (challenge_id, user_id, entry_id)
);

CREATE INDEX idx_challenge_votes_entry ON challenge_votes (challenge_id, entry_id);

-- The candidates of the leaderboard (catalog/leaderboards.go), which parses
-- and ranks them in Go: complete analyses of space platforms (the ship
-- boards) and of builds that make science packs or more electricity than
-- they use (the factory boards). ListBoardCandidates repeats this predicate
-- word for word, so the planner uses the index.
CREATE INDEX idx_blueprint_stats_board_candidates ON blueprint_stats (blueprint_id)
    WHERE production_state = 'complete'
      AND (production @> '{"space_platform": {"is_space_platform": true}}'
           OR (production->'electricity'->>'net_watts')::double precision > 0
           OR jsonb_path_exists(production, '$.products[*] ? (@.name like_regex "-science-pack$")')
           OR jsonb_path_exists(production, '$.intermediates[*] ? (@.name like_regex "-science-pack$")'));

-- The current leader of each leaderboard board (catalog/leaderboards.go),
-- refreshed every minute by the leaderboards task: no history, only who leads
-- now and since when (since changes when another entry takes the lead). A
-- board with fewer than 10 eligible entries has no row. user_id is the
-- entry's owner, null for an anonymous entry.
CREATE TABLE board_leaders (
    board        text PRIMARY KEY
                     CHECK (board IN ('fastest', 'thrust_to_weight', 'lightest', 'most_cargo',
                                      'most_science', 'densest_science', 'cleanest_science',
                                      'biggest_power_plant')),
    entry_id     short_id NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
    blueprint_id short_id NOT NULL REFERENCES blueprints (id) ON DELETE CASCADE,
    user_id      short_id REFERENCES users (id) ON DELETE SET NULL,
    value        double precision NOT NULL,
    since        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_board_leaders_user ON board_leaders (user_id) WHERE user_id IS NOT NULL;

-- The XP ledger (docs/architecture.md#xp-and-badges): append-only, never
-- updated. source_key names the event that earned the points ('upload:<entry>',
-- 'favorite:<user>:<entry>', 'challenge:<challenge>:place:<user>', ...), so a
-- retry, a repeat or a restore can never award twice. A row counts towards the
-- total only while its content lives (entry_id: the entry is live; comment_id:
-- the comment is not removed); records and challenge awards keep their
-- badges either way. No foreign keys to entries or comments, so a hard delete
-- leaves the row (it just stops counting); the rows go with the account.
CREATE TABLE xp_events (
    id           short_id PRIMARY KEY,
    user_id      short_id NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason       text NOT NULL CHECK (reason IN (
                     'upload', 'favorite', 'collection', 'comment', 'record',
                     'challenge_win', 'challenge_second', 'challenge_third', 'challenge_participation')),
    points       integer NOT NULL CHECK (points > 0),
    source_key   text NOT NULL UNIQUE CHECK (char_length(source_key) BETWEEN 1 AND 200),
    entry_id     short_id,
    comment_id   short_id,
    challenge_id short_id,
    board        text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_xp_events_user ON xp_events (user_id, reason, created_at);

-- The home page's hand-picked leaves (at most six, enforced by the staff
-- operation under a table lock): a blueprint with an optional one-line
-- caption saying why. Shown newest first while the leaf is in the current
-- revision of a listed entry; otherwise it drops out (staff see it as hidden).
CREATE TABLE home_picks (
    blueprint_id short_id PRIMARY KEY REFERENCES blueprints (id) ON DELETE CASCADE,
    caption      text NOT NULL DEFAULT '' CHECK (char_length(caption) <= 140 AND caption !~ '[\r\n]'),
    picked_at    timestamptz NOT NULL DEFAULT now(),
    -- The staff member's console username.
    picked_by    text NOT NULL CHECK (picked_by <> '')
);

-- The staff audit log: one row per staff write, written in the same
-- transaction as the change. staff_username is the console username (staff
-- identities are not expo accounts); target_id is an id, never personal data.
-- before/after hold the changed fields. Staff-only: never exposed publicly.
CREATE TABLE admin_actions (
    id             short_id PRIMARY KEY,
    staff_username text NOT NULL CHECK (staff_username <> ''),
    action         text NOT NULL CHECK (action <> ''),
    target_type    text NOT NULL CHECK (target_type <> ''),
    target_id      text NOT NULL,
    reason         text NOT NULL DEFAULT '',
    before         jsonb,
    after          jsonb,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_actions_created ON admin_actions (created_at DESC, id DESC);

-- Global settings: the emergency switches, the site banner and other values
-- staff change at runtime (docs/admin.md#settings). Keys and their types are
-- defined in code (internal/settings); a missing row means the key's default,
-- an unknown key is ignored. value is the text form of a boolean, number or
-- string. updated_by is the console username of the last change.
CREATE TABLE global_settings (
    key        text PRIMARY KEY CHECK (key <> ''),
    value      text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT ''
);
