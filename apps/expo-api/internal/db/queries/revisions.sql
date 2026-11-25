-- name: CreateRevision :one
INSERT INTO revisions (
    id, entry_id, version_number, original_string, tree, game_version, compat_21, note
)
VALUES (
    $1, $2,
    (SELECT coalesce(max(version_number), 0) + 1 FROM revisions WHERE entry_id = $2::short_id),
    $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetRevision :one
SELECT * FROM revisions WHERE id = $1;

-- name: ListRevisionsByEntry :many
SELECT * FROM revisions
WHERE entry_id = $1
ORDER BY version_number DESC;

-- name: DeleteRevision :execrows
DELETE FROM revisions WHERE id = $1 AND entry_id = $2;

-- name: GetNodeRevision :one
-- The revision and entry a book, blueprint or planner row belongs to (ids are
-- unique across the three tables).
SELECT revisions.id AS revision_id, revisions.entry_id
FROM (
    SELECT revision_id FROM blueprints WHERE blueprints.id = $1
    UNION ALL SELECT revision_id FROM books WHERE books.id = $1
    UNION ALL SELECT revision_id FROM planners WHERE planners.id = $1
) AS node
JOIN revisions ON revisions.id = node.revision_id
LIMIT 1;
