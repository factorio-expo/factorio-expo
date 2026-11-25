-- name: GetEntryAlias :one
SELECT slug FROM entry_aliases WHERE entry_id = $1;

-- name: ListEntryAliases :many
-- The aliases of the given entries (an entry without one has no row), so a
-- listing can carry them without a query per entry.
SELECT entry_aliases.entry_id, entry_aliases.slug
FROM entry_aliases
WHERE entry_aliases.entry_id = ANY (sqlc.arg(ids)::text[]);

-- name: DeleteStaleAlias :execrows
-- Frees a slug whose entry is no longer live (deleted or expired) so its user
-- can use it again; a live entry's alias never matches.
DELETE FROM entry_aliases
USING entries
WHERE entries.id = entry_aliases.entry_id
  AND entry_aliases.user_id = $1
  AND entry_aliases.slug = $2
  AND NOT entry_live(entries);

-- name: SetEntryAlias :exec
-- Sets (or replaces) the entry's alias; a slug another live entry of the user
-- already has violates the (user_id, slug) unique constraint.
INSERT INTO entry_aliases (entry_id, user_id, slug)
VALUES ($1, $2, $3)
ON CONFLICT (entry_id) DO UPDATE SET slug = excluded.slug, user_id = excluded.user_id;

-- name: DeleteEntryAlias :execrows
DELETE FROM entry_aliases WHERE entry_id = $1;

-- name: GetEntryIDByAlias :one
-- The live entry a user's alias names (case-insensitive username); visibility
-- is the caller's to check.
SELECT entries.id
FROM entry_aliases
JOIN users ON users.id = entry_aliases.user_id AND users.deleted_at IS NULL
JOIN entries ON entries.id = entry_aliases.entry_id AND entry_live(entries)
WHERE lower(users.username) = lower(sqlc.arg(username)::text)
  AND entry_aliases.slug = sqlc.arg(slug)::text;
