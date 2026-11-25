-- name: CreateEntry :one
INSERT INTO entries (id, owner_user_id, edit_token_hash, visibility, title, description, expires_at, quick_preview, spoiler, source_url)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: SetEntryCurrentRevision :one
UPDATE entries
SET current_revision_id = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetEntry :one
-- A live entry (entry_live). An expired entry reads as missing until the
-- sweep removes it.
SELECT *
FROM entries
WHERE id = $1
  AND entry_live(entries);

-- name: GetListedEntry :one
-- A listed entry (entry_listed): what the search projection indexes.
SELECT *
FROM entries
WHERE id = $1
  AND entry_listed(entries);

-- name: GetEntryForUpdate :one
SELECT *
FROM entries
WHERE id = $1
  AND entry_live(entries)
FOR UPDATE;

-- name: UpdateEntryMetadata :one
UPDATE entries
SET title = coalesce(sqlc.narg(title)::text, title),
    description = coalesce(sqlc.narg(description)::text, description),
    visibility = coalesce(sqlc.narg(visibility)::text, visibility),
    spoiler = coalesce(sqlc.narg(spoiler)::boolean, spoiler),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND entry_live(entries)
RETURNING *;

-- name: DeleteEntry :execrows
-- Soft delete only: entries are never hard-removed so revisions, nodes, and
-- previews survive and entry_search_jobs can still reference the row to
-- issue a Typesense removal.
UPDATE entries
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1
  AND deleted_at IS NULL;

-- name: ListEntryCards :many
-- The one entry-card projection, shared by every listing (profile, favorites,
-- collections) and by search result cards. Each listing's scoping query picks
-- and orders the ids; cards come back in that order, and only for entries that
-- are live and visible to the viewer (a lagging search index or a concurrent
-- delete just drops a card). The uploader's name and the favorites count are
-- read here rather than projected into Typesense: they change independently of
-- the facts worth reindexing on.
SELECT
    entries.id,
    entries.owner_user_id,
    entries.visibility,
    entries.title,
    entries.spoiler,
    users.username           AS owner_username,
    entries.created_at,
    entries.updated_at,
    entries.expires_at,
    entry_listed(entries)::boolean AS listed,
    revisions.id             AS revision_id,
    revisions.version_number,
    revisions.game_version,
    revisions.compat_21,
    coalesce(root_book.label, root_blueprint.label, root_planner.label, '')                   AS root_label,
    coalesce(root_book.description, root_blueprint.description, root_planner.description, '') AS root_description,
    coalesce(root_book.icons, root_blueprint.icons, root_planner.icons, '')                   AS root_icons,
    (revisions.tree->>'kind')::text AS root_kind,
    leaf_totals.leaf_count,
    leaf_totals.preview_count,
    favorite_totals.favorites_count,
    coalesce(entry_view_counts.views, 0)::bigint AS view_count,
    coalesce(cover.blueprint_id, '')::text AS cover_blueprint_id,
    -- Some leaf's preview phase is pending: the same rule as progress.phaseOf
    -- (a pending job, or neither a stored preview nor a failed job).
    EXISTS (
        SELECT 1
        FROM leaf_progress
        WHERE leaf_progress.revision_id = revisions.id
          AND (leaf_progress.preview_job_state = 'pending'
               OR (NOT leaf_progress.has_preview AND leaf_progress.preview_job_state <> 'failed'))
    )::boolean AS preview_pending
FROM unnest(sqlc.arg(ids)::text[]) WITH ORDINALITY AS wanted(id, position)
JOIN entries ON entries.id = wanted.id
LEFT JOIN users ON users.id = entries.owner_user_id
LEFT JOIN entry_view_counts ON entry_view_counts.entry_id = entries.id
JOIN revisions ON revisions.id = entries.current_revision_id
LEFT JOIN books      AS root_book      ON root_book.id      = (revisions.tree->>'id')
LEFT JOIN blueprints AS root_blueprint ON root_blueprint.id = (revisions.tree->>'id')
LEFT JOIN planners   AS root_planner   ON root_planner.id   = (revisions.tree->>'id')
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS leaf_count,
           count(previews.blueprint_id)::bigint AS preview_count
    FROM blueprints
    LEFT JOIN previews ON previews.blueprint_id = blueprints.id
    WHERE blueprints.revision_id = revisions.id
) AS leaf_totals ON true
LEFT JOIN LATERAL (
    SELECT count(*)::bigint AS favorites_count
    FROM favorites
    WHERE favorites.entry_id = entries.id
) AS favorite_totals ON true
LEFT JOIN LATERAL (
    SELECT previews.blueprint_id
    FROM blueprints
    JOIN previews ON previews.blueprint_id = blueprints.id
    WHERE blueprints.revision_id = revisions.id
    ORDER BY blueprints.position, blueprints.id
    LIMIT 1
) AS cover ON true
-- Quick previews never get a card, wherever the ids came from.
WHERE entry_lasting(entries)
  -- Unlisted entries are viewable by anyone; only the id-picking queries
  -- decide whether they are listed.
  AND entry_visible_to(entries, sqlc.narg(viewer_user_id)::text)
ORDER BY wanted.position;

-- name: ListBookCoverBlueprintIDs :many
-- The collage of a book card: up to four leaf previews of each given entry's
-- current revision, in leaf order (the card's cover is the first).
SELECT entries.id AS entry_id,
       coalesce((array_agg(previews.blueprint_id ORDER BY blueprints.position, blueprints.id))[1:4],
                '{}')::text[] AS blueprint_ids
FROM entries
JOIN blueprints ON blueprints.revision_id = entries.current_revision_id
JOIN previews ON previews.blueprint_id = blueprints.id
WHERE entries.id = ANY (sqlc.arg(ids)::text[])
GROUP BY entries.id;

-- name: ListFavoritedEntryIDs :many
-- Which of the given entry ids the given user has favorited, so search
-- result cards can flag is_favorited without a per-row query.
SELECT entry_id
FROM favorites
WHERE user_id = $1
  AND entry_id = ANY (sqlc.arg(ids)::text[]);

-- name: ListOwnerPublicEntryIDs :many
-- One user's public, live entries, newest first; cards via ListEntryCards.
SELECT id
FROM entries
WHERE owner_user_id = $1
  AND entry_listed(entries)
ORDER BY created_at DESC, id
LIMIT $2 OFFSET $3;

-- name: CountOwnerPublicEntries :one
SELECT count(*)::bigint
FROM entries
WHERE owner_user_id = $1
  AND entry_listed(entries);

-- name: ListOwnerEntryIDs :many
-- All of one user's live entries, of any visibility, newest first: the
-- owner's own listing (cards via ListEntryCards, which shows private entries
-- only to their owner).
SELECT id
FROM entries
WHERE owner_user_id = $1
  AND entry_lasting(entries)
  AND current_revision_id IS NOT NULL
ORDER BY created_at DESC, id
LIMIT $2 OFFSET $3;

-- name: CountOwnerEntries :one
SELECT count(*)::bigint
FROM entries
WHERE owner_user_id = $1
  AND entry_lasting(entries)
  AND current_revision_id IS NOT NULL;

-- name: ListOwnerBlueprintEntryIDs :many
-- One user's listed (public) entries whose root is a single blueprint, newest
-- first, whose title contains the search text (case-insensitive; empty matches
-- all): what a challenge accepts as an entry.
SELECT entries.id
FROM entries
JOIN revisions ON revisions.id = entries.current_revision_id
WHERE entries.owner_user_id = sqlc.arg(owner_user_id)
  AND entry_listed(entries)
  AND revisions.tree->>'kind' = 'blueprint'
  AND strpos(lower(entries.title), lower(sqlc.arg(search)::text)) > 0
ORDER BY entries.created_at DESC, entries.id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountOwnerBlueprintEntries :one
SELECT count(*)::bigint
FROM entries
JOIN revisions ON revisions.id = entries.current_revision_id
WHERE entries.owner_user_id = sqlc.arg(owner_user_id)
  AND entry_listed(entries)
  AND revisions.tree->>'kind' = 'blueprint'
  AND strpos(lower(entries.title), lower(sqlc.arg(search)::text)) > 0;

-- name: PublishEntry :one
-- Gives a live quick preview or anonymous entry to an account: it gets its
-- owner, entry metadata and expiry (the caller decides: a preview's own
-- expiry never survives), stops being a quick preview and loses its edit
-- token (ownership governs edits from now on). Only a live preview or
-- ownerless entry matches.
UPDATE entries
SET owner_user_id = sqlc.arg(owner_user_id),
    edit_token_hash = NULL,
    visibility = sqlc.arg(visibility),
    title = sqlc.arg(title),
    description = sqlc.arg(description),
    expires_at = sqlc.narg(expires_at),
    quick_preview = false,
    spoiler = coalesce(sqlc.narg(spoiler)::boolean, spoiler),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND entry_live(entries)
  AND (quick_preview OR owner_user_id IS NULL)
RETURNING *;

-- name: RejectQuickPreview :execrows
-- Hard-deletes a quick preview whose render or analysis failed for good; the
-- cascade removes its revision, nodes, stats, preview and job rows. Other
-- entries never match.
DELETE FROM entries
WHERE id = $1
  AND quick_preview;

-- name: DeleteExpiredPreviews :many
-- The minute sweep, quick previews: hard-deletes expired ones in bounded
-- batches and returns their ids. They were never indexed, so no search
-- removal is needed.
DELETE FROM entries
WHERE id IN (
    SELECT id FROM entries
    WHERE quick_preview
      AND expires_at <= now()
    ORDER BY expires_at
    LIMIT 500
)
RETURNING id;

-- name: ExpireEntries :many
-- The minute sweep, uploads: soft-deletes expired entries in bounded batches,
-- as a delete would, and returns their ids (the catalog queues their search
-- removal in the same transaction).
UPDATE entries
SET deleted_at = now(),
    updated_at = now()
WHERE id IN (
    SELECT id FROM entries
    WHERE NOT quick_preview
      AND deleted_at IS NULL
      AND expires_at <= now()
    ORDER BY expires_at
    LIMIT 500
    FOR UPDATE SKIP LOCKED
)
RETURNING id;

-- name: ListSitemapEntries :many
-- One page of the sitemap: listed entries (the same scope as search) after
-- the given id, in id order so pages are stable while entries change.
SELECT id, updated_at
FROM entries
WHERE entry_listed(entries)
  AND id > sqlc.arg(after_id)::text
ORDER BY id
LIMIT sqlc.arg(page_limit);

-- name: ListLeafCardPreviews :many
-- Preview state of the given leaves for search result cards: only leaves of
-- an entry's current revision come back, so a leaf a newer revision replaced
-- (index lagging) drops its card. preview_pending is the same rule as
-- ListEntryCards.
SELECT
    leaf_progress.blueprint_id,
    leaf_progress.has_preview,
    (leaf_progress.preview_job_state = 'pending'
     OR (NOT leaf_progress.has_preview AND leaf_progress.preview_job_state <> 'failed'))::boolean AS preview_pending
FROM leaf_progress
JOIN entries ON entries.current_revision_id = leaf_progress.revision_id
WHERE leaf_progress.blueprint_id = ANY (sqlc.arg(ids)::text[]);
