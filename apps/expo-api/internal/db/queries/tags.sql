-- name: ListTags :many
-- Tags are a curated, admin-defined set (see the seed rows in
-- 000002_seed_initial_data.up.sql); this only reads them, it never writes.
SELECT * FROM tags ORDER BY position, display_name;

-- name: ListTagGroups :many
SELECT * FROM tag_groups ORDER BY position;

-- name: ListTagsByEntry :many
-- Resolves the curated tag slugs assigned to one entry, for building the
-- search projection's `tags` facet.
SELECT tags.*
FROM entry_tags
         JOIN tags ON tags.id = entry_tags.tag_id
WHERE entry_tags.entry_id = $1
ORDER BY tags.display_name;

-- name: ListTagsBySlugs :many
SELECT slug
FROM tags
WHERE slug = ANY (sqlc.arg(slugs)::text[]);

-- name: SetEntryTags :exec
INSERT INTO entry_tags (entry_id, tag_id)
SELECT $1, tags.id
FROM tags
WHERE tags.slug = ANY (sqlc.arg(slugs)::text[])
ON CONFLICT DO NOTHING;

-- name: ClearEntryTags :exec
-- Used with SetEntryTags to replace an entry's whole tag set.
DELETE FROM entry_tags WHERE entry_id = $1;

-- name: ListTagSuggestionRules :many
-- Every tag suggestion rule (seeded in 000002_seed_initial_data.up.sql), its tags in
-- curated order, so tied suggestions keep that order.
SELECT tags.slug AS tag_slug, tag_groups.slug AS group_slug, rules.kind, rules.name, rules.min_count, rules.max_count, rules.points
FROM tag_suggestion_rules rules
         JOIN tags ON tags.id = rules.tag_id
         JOIN tag_groups ON tag_groups.id = tags.group_id
ORDER BY tag_groups.position, tags.position, rules.kind, rules.name;
