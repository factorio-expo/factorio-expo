-- name: ListUserFeedItems :many
-- The user's feed: the newest revisions of their listed entries (live, public,
-- not quick previews), newest first. A revision is the entry's first when no
-- earlier one is left; the title falls back to the root node's label.
SELECT entries.id AS entry_id,
       coalesce(nullif(entries.title, ''), root_book.label, root_blueprint.label, root_planner.label, '')::text AS title,
       entries.description,
       revisions.id AS revision_id,
       revisions.version_number,
       revisions.note,
       revisions.created_at,
       (revisions.version_number = (
           SELECT min(earliest.version_number) FROM revisions earliest WHERE earliest.entry_id = entries.id
       ))::boolean AS first_revision
FROM entries
JOIN revisions ON revisions.entry_id = entries.id
JOIN revisions live ON live.id = entries.current_revision_id
LEFT JOIN books      AS root_book      ON root_book.id      = (live.tree->>'id')
LEFT JOIN blueprints AS root_blueprint ON root_blueprint.id = (live.tree->>'id')
LEFT JOIN planners   AS root_planner   ON root_planner.id   = (live.tree->>'id')
WHERE entries.owner_user_id = sqlc.arg(owner_user_id)::text
  AND entry_listed(entries)
ORDER BY revisions.created_at DESC, revisions.id
LIMIT sqlc.arg(item_limit);
