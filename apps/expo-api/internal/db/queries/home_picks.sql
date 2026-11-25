-- name: ListShownHomePicks :many
-- The home page's picks that show: the leaf is in the current revision of a
-- listed entry. Newest pick first.
SELECT home_picks.blueprint_id,
       home_picks.caption,
       home_picks.picked_at,
       entries.id AS entry_id,
       entries.title AS entry_title,
       coalesce(revisions.tree->>'kind', '')::text AS root_kind,
       blueprints.label,
       blueprints.description,
       blueprints.icons
FROM home_picks
JOIN blueprints ON blueprints.id = home_picks.blueprint_id
JOIN revisions ON revisions.id = blueprints.revision_id
JOIN entries ON entries.id = revisions.entry_id
WHERE entries.current_revision_id = revisions.id
  AND entry_listed(entries)
ORDER BY home_picks.picked_at DESC, home_picks.blueprint_id;

-- name: ListStaffHomePicks :many
-- Every pick for the console, newest first, with whether it shows.
SELECT home_picks.blueprint_id,
       home_picks.caption,
       home_picks.picked_at,
       home_picks.picked_by,
       entries.id AS entry_id,
       entries.title AS entry_title,
       blueprints.label,
       (entries.current_revision_id = revisions.id AND entry_listed(entries))::boolean AS shown
FROM home_picks
JOIN blueprints ON blueprints.id = home_picks.blueprint_id
JOIN revisions ON revisions.id = blueprints.revision_id
JOIN entries ON entries.id = revisions.entry_id
ORDER BY home_picks.picked_at DESC, home_picks.blueprint_id;

-- name: LockHomePicks :exec
-- Serializes pick additions, so the count check holds.
SELECT pg_advisory_xact_lock(hashtextextended('home_picks', 0));

-- name: CountHomePicks :one
SELECT count(*)::bigint FROM home_picks;

-- name: GetPickableLeaf :one
-- A blueprint that may be picked: in the current revision of a listed entry.
SELECT blueprints.id, revisions.entry_id
FROM blueprints
JOIN revisions ON revisions.id = blueprints.revision_id
JOIN entries ON entries.id = revisions.entry_id
WHERE blueprints.id = $1
  AND entries.current_revision_id = revisions.id
  AND entry_listed(entries);

-- name: InsertHomePick :execrows
INSERT INTO home_picks (blueprint_id, caption, picked_by)
VALUES ($1, $2, $3)
ON CONFLICT (blueprint_id) DO NOTHING;

-- name: UpdateHomePickCaption :one
-- Changes a pick's caption; returns the old one for the audit row.
UPDATE home_picks AS updated
SET caption = sqlc.arg(caption)
FROM home_picks AS old
WHERE updated.blueprint_id = sqlc.arg(blueprint_id) AND old.blueprint_id = updated.blueprint_id
RETURNING old.caption;

-- name: DeleteHomePick :one
DELETE FROM home_picks WHERE blueprint_id = $1 RETURNING caption;
