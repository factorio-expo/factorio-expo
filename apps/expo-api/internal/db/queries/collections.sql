-- name: CreateCollection :one
INSERT INTO collections (id, user_id, name, description, visibility)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCollection :one
SELECT * FROM collections WHERE id = $1;

-- name: ListCollectionsByUser :many
SELECT * FROM collections WHERE user_id = $1 ORDER BY created_at DESC;

-- name: ListCollectionMembershipByUser :many
-- The user's collections, newest first, each with whether it holds one entry.
SELECT sqlc.embed(collections),
       EXISTS (
           SELECT 1 FROM collection_entries
           WHERE collection_entries.collection_id = collections.id
             AND collection_entries.entry_id = sqlc.arg(entry_id)
       ) AS contains
FROM collections
WHERE collections.user_id = sqlc.arg(user_id)
ORDER BY collections.created_at DESC;

-- name: UpdateCollection :one
UPDATE collections
SET name = $2,
    description = $3,
    visibility = $4
WHERE id = $1
RETURNING *;

-- name: DeleteCollection :execrows
DELETE FROM collections WHERE id = $1;

-- name: AddCollectionEntry :execrows
-- Upsert: re-adding an entry already in the collection just moves it to the
-- given position instead of failing.
INSERT INTO collection_entries (collection_id, entry_id, position)
VALUES ($1, $2, $3)
ON CONFLICT (collection_id, entry_id) DO UPDATE SET position = EXCLUDED.position;

-- name: SetCollectionEntryPosition :execrows
-- Moves a member entry (never adds one): the owner's drag-and-drop order.
UPDATE collection_entries SET position = $3
WHERE collection_id = $1 AND entry_id = $2;

-- name: RemoveCollectionEntry :execrows
DELETE FROM collection_entries WHERE collection_id = $1 AND entry_id = $2;

-- name: ListCollectionEntryIDs :many
-- One collection's live entries the viewer may see, in its stored order
-- (unlisted ones included: the collection owner chose to share their links);
-- cards via ListEntryCards.
SELECT entries.id
FROM collection_entries
JOIN entries ON entries.id = collection_entries.entry_id
WHERE collection_entries.collection_id = sqlc.arg(collection_id)
  AND entry_lasting(entries)
  AND entries.current_revision_id IS NOT NULL
  AND entry_visible_to(entries, sqlc.narg(viewer_user_id)::text)
ORDER BY collection_entries.position, collection_entries.added_at;

-- name: ListPublicCollectionsByUser :many
SELECT * FROM collections
WHERE user_id = $1 AND visibility = 'public'
ORDER BY created_at DESC;

-- name: ListCollectionSummaries :many
-- What a collection card shows, for each given collection: how many of its
-- entries the viewer may see (as ListCollectionEntryIDs counts them) and the
-- cover thumbnails of the first four that have one, in the collection's
-- order. Spoiler entries give no cover.
SELECT collection_entries.collection_id,
       count(*)::bigint AS entry_count,
       coalesce((array_agg(cover.blueprint_id ORDER BY collection_entries.position, collection_entries.added_at)
                 FILTER (WHERE cover.blueprint_id IS NOT NULL AND NOT entries.spoiler))[1:4],
                '{}')::text[] AS cover_blueprint_ids
FROM collection_entries
JOIN entries ON entries.id = collection_entries.entry_id
LEFT JOIN LATERAL (
    SELECT previews.blueprint_id
    FROM blueprints
    JOIN previews ON previews.blueprint_id = blueprints.id
    WHERE blueprints.revision_id = entries.current_revision_id
    ORDER BY blueprints.position, blueprints.id
    LIMIT 1
) AS cover ON true
WHERE collection_entries.collection_id = ANY(sqlc.arg(ids)::text[])
  AND entry_lasting(entries)
  AND entries.current_revision_id IS NOT NULL
  AND entry_visible_to(entries, sqlc.narg(viewer_user_id)::text)
GROUP BY collection_entries.collection_id;
