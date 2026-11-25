-- Staff console -> Tags and Reserved usernames (admin listener only): the
-- curated tag groups, tags and tag suggestion rules, and the reserved
-- usernames. Seeded by 000002_seed_initial_data; edited here afterwards.

-- name: ListStaffTagGroups :many
SELECT g.*, (SELECT count(*) FROM tags t WHERE t.group_id = g.id)::bigint AS tags
FROM tag_groups g
ORDER BY g.position, g.display_name;

-- name: ListStaffTags :many
-- Every tag with how many entries carry it and its suggestion rule count.
SELECT t.*,
       (SELECT count(*) FROM entry_tags et WHERE et.tag_id = t.id)::bigint AS entries,
       (SELECT count(*) FROM tag_suggestion_rules r WHERE r.tag_id = t.id)::bigint AS rules
FROM tags t
JOIN tag_groups g ON g.id = t.group_id
ORDER BY g.position, t.position, t.display_name;

-- name: ListStaffTagRules :many
SELECT * FROM tag_suggestion_rules ORDER BY tag_id, kind, name;

-- name: CreateTagGroup :one
-- The id derives from the slug, as the seed's do.
INSERT INTO tag_groups (id, slug, display_name, position)
VALUES (left(md5('tag-group:' || sqlc.arg(slug)::text), 12), sqlc.arg(slug), sqlc.arg(display_name), sqlc.arg(position))
RETURNING *;

-- name: UpdateTagGroup :execrows
UPDATE tag_groups SET display_name = sqlc.arg(display_name), position = sqlc.arg(position)
WHERE id = sqlc.arg(id);

-- name: DeleteEmptyTagGroup :execrows
DELETE FROM tag_groups g
WHERE g.id = $1 AND NOT EXISTS (SELECT 1 FROM tags t WHERE t.group_id = g.id);

-- name: CountTagsInGroup :one
SELECT count(*)::bigint FROM tags WHERE group_id = $1;

-- name: CreateTag :one
-- The id derives from the slug, as the seed's do.
INSERT INTO tags (id, group_id, slug, display_name, icon, position)
VALUES (left(md5('tag:' || sqlc.arg(slug)::text), 12), sqlc.arg(group_id), sqlc.arg(slug), sqlc.arg(display_name), sqlc.arg(icon), sqlc.arg(position))
RETURNING *;

-- name: UpdateTag :execrows
-- Slugs never change: search documents and links hold them.
UPDATE tags SET group_id = sqlc.arg(group_id), display_name = sqlc.arg(display_name), icon = sqlc.arg(icon), position = sqlc.arg(position)
WHERE id = sqlc.arg(id);

-- name: GetTagForUpdate :one
SELECT * FROM tags WHERE id = $1 FOR UPDATE;

-- name: DeleteTag :execrows
-- Its entry_tags rows and suggestion rules cascade.
DELETE FROM tags WHERE id = $1;

-- name: UpsertTagRule :exec
INSERT INTO tag_suggestion_rules (tag_id, kind, name, min_count, max_count, points)
VALUES (sqlc.arg(tag_id), sqlc.arg(kind), sqlc.arg(name), sqlc.arg(min_count), sqlc.narg(max_count), sqlc.arg(points))
ON CONFLICT (tag_id, kind, name) DO UPDATE
    SET min_count = EXCLUDED.min_count, max_count = EXCLUDED.max_count, points = EXCLUDED.points;

-- name: DeleteTagRule :execrows
DELETE FROM tag_suggestion_rules
WHERE tag_id = sqlc.arg(tag_id) AND kind = sqlc.arg(kind) AND name = sqlc.arg(name);

-- name: ListReservedUsernames :many
-- Every reserved name with the account that already has it, if any (a name
-- reserved after the sign-up stays that account's).
SELECT r.name, r.reason, r.created_at,
       coalesce((SELECT u.username FROM users u
                 WHERE translate(lower(u.username), '_-', '') = r.name
                 ORDER BY u.created_at LIMIT 1), '')::text AS taken_by
FROM reserved_usernames r
ORDER BY r.name;

-- name: AddReservedUsername :one
-- name is already normalized (lower case, no '_' or '-').
INSERT INTO reserved_usernames (name, reason) VALUES (sqlc.arg(name), sqlc.arg(reason))
ON CONFLICT (name) DO UPDATE SET reason = EXCLUDED.reason
RETURNING (xmax = 0)::boolean AS inserted;

-- name: DeleteReservedUsername :execrows
DELETE FROM reserved_usernames WHERE name = $1;
