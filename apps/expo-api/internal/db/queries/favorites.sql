-- name: CreateFavorite :execrows
-- Idempotent: favoriting an already-favorited entry is a no-op, not an error.
INSERT INTO favorites (user_id, entry_id)
VALUES ($1, $2)
ON CONFLICT (user_id, entry_id) DO NOTHING;

-- name: DeleteFavorite :execrows
DELETE FROM favorites WHERE user_id = $1 AND entry_id = $2;

-- name: ListFavoriteEntryIDs :many
-- One user's live favorites, newest favorite first; cards via ListEntryCards.
SELECT entries.id
FROM favorites
JOIN entries ON entries.id = favorites.entry_id
WHERE favorites.user_id = $1
  AND entry_lasting(entries)
  AND entries.current_revision_id IS NOT NULL
  -- A favorite never reveals an entry its owner has made private.
  AND entry_visible_to(entries, $1)
ORDER BY favorites.created_at DESC, entries.id
LIMIT $2 OFFSET $3;

-- name: CountFavoriteEntries :one
-- ListFavoriteEntryIDs' total, for paging.
SELECT count(*)::bigint
FROM favorites
JOIN entries ON entries.id = favorites.entry_id
WHERE favorites.user_id = $1
  AND entry_lasting(entries)
  AND entries.current_revision_id IS NOT NULL
  AND entry_visible_to(entries, $1);
