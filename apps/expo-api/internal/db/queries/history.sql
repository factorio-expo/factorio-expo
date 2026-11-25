-- name: ListSingleBlueprintRevisions :many
-- The revisions of an entry that hold exactly one blueprint and nothing else
-- (no book, no planner), oldest first, with that blueprint: the steps of the
-- preview viewer's revision timeline.
SELECT revisions.id AS revision_id,
       revisions.version_number,
       revisions.game_version,
       revisions.created_at,
       revisions.note,
       blueprints.id AS blueprint_id
FROM revisions
JOIN blueprints ON blueprints.revision_id = revisions.id
WHERE revisions.entry_id = $1
  AND (SELECT count(*) FROM blueprints own WHERE own.revision_id = revisions.id) = 1
  AND NOT EXISTS (SELECT 1 FROM books WHERE books.revision_id = revisions.id)
  AND NOT EXISTS (SELECT 1 FROM planners WHERE planners.revision_id = revisions.id)
ORDER BY revisions.version_number;
