-- The public profile (catalog/profile.go): status line, social links, pinned
-- entries, stats and the user's comments. Everything a profile shows comes
-- from listed entries (entry_listed) only.

-- name: SetUserStatus :exec
UPDATE users SET status = $2 WHERE id = $1;

-- name: ListSocialLinks :many
SELECT platform, handle
FROM user_social_links
WHERE user_id = $1
ORDER BY platform;

-- name: DeleteSocialLinks :execrows
DELETE FROM user_social_links WHERE user_id = $1;

-- name: InsertSocialLink :exec
INSERT INTO user_social_links (user_id, platform, handle) VALUES ($1, $2, $3);

-- name: GetProfileStats :one
-- The profile's numbers over the user's listed entries: how many, their
-- all-time views, and the favorites they received from other users.
SELECT count(*)::bigint AS entries,
       coalesce(sum(entry_view_counts.views), 0)::bigint AS views,
       (SELECT count(*)
        FROM favorites
        JOIN entries AS favorited ON favorited.id = favorites.entry_id
        WHERE favorited.owner_user_id = sqlc.arg(user_id)::text
          AND favorites.user_id <> sqlc.arg(user_id)::text
          AND entry_listed(favorited))::bigint AS favorites
FROM entries
LEFT JOIN entry_view_counts ON entry_view_counts.entry_id = entries.id
WHERE entries.owner_user_id = sqlc.arg(user_id)::text
  AND entry_listed(entries);

-- name: ListProfileTopTags :many
-- The tags the user's listed entries carry most, most used first.
SELECT tags.slug, tags.display_name, tags.icon, count(*)::bigint AS entries
FROM entries
JOIN entry_tags ON entry_tags.entry_id = entries.id
JOIN tags ON tags.id = entry_tags.tag_id
WHERE entries.owner_user_id = sqlc.arg(user_id)::text
  AND entry_listed(entries)
GROUP BY tags.id, tags.slug, tags.display_name, tags.icon
ORDER BY entries DESC, tags.slug
LIMIT sqlc.arg(row_limit)::integer;

-- name: ListPinnedEntryIDs :many
-- The user's pins that still show, in the user's order: their own listed
-- entries. Pins of entries no longer listed are skipped (pruned on the next
-- pin change).
SELECT user_pinned_entries.entry_id
FROM user_pinned_entries
JOIN entries ON entries.id = user_pinned_entries.entry_id
WHERE user_pinned_entries.user_id = $1
  AND entries.owner_user_id = user_pinned_entries.user_id
  AND entry_listed(entries)
ORDER BY user_pinned_entries.position, user_pinned_entries.pinned_at;

-- name: ListPinnedAmong :many
-- Which of the given entry ids the user has pinned (the is_pinned flags).
SELECT entry_id
FROM user_pinned_entries
WHERE user_id = sqlc.arg(user_id)::text
  AND entry_id = ANY(sqlc.arg(ids)::text[]);

-- name: ListPinnableAmong :many
-- Which of the given entry ids the user may pin: their own listed entries.
SELECT id
FROM entries
WHERE id = ANY(sqlc.arg(ids)::text[])
  AND owner_user_id = sqlc.arg(user_id)::text
  AND entry_listed(entries);

-- name: DeleteUserPins :execrows
DELETE FROM user_pinned_entries WHERE user_id = $1;

-- name: InsertPin :exec
INSERT INTO user_pinned_entries (user_id, entry_id, position) VALUES ($1, $2, $3);

-- name: ListUserComments :many
-- One page of the user's comments on listed entries, newest first; removed
-- ones are left out. entry_title falls back to the root node's label.
SELECT comments.entry_id,
       comments.number,
       comments.body,
       comments.created_at,
       comments.edited_at,
       coalesce(nullif(entries.title, ''), root_book.label, root_blueprint.label, root_planner.label, '')::text
           AS entry_title
FROM comments
JOIN entries ON entries.id = comments.entry_id
JOIN revisions ON revisions.id = entries.current_revision_id
LEFT JOIN books      AS root_book      ON root_book.id      = (revisions.tree->>'id')
LEFT JOIN blueprints AS root_blueprint ON root_blueprint.id = (revisions.tree->>'id')
LEFT JOIN planners   AS root_planner   ON root_planner.id   = (revisions.tree->>'id')
WHERE comments.author_user_id = $1
  AND comments.deleted_at IS NULL
  AND entry_listed(entries)
ORDER BY comments.created_at DESC, comments.id
LIMIT $2 OFFSET $3;

-- name: CountUserComments :one
SELECT count(*)::bigint
FROM comments
JOIN entries ON entries.id = comments.entry_id
WHERE comments.author_user_id = $1
  AND comments.deleted_at IS NULL
  AND entry_listed(entries);

-- name: CountUserPublicCollections :one
SELECT count(*)::bigint FROM collections WHERE user_id = $1 AND visibility = 'public';
