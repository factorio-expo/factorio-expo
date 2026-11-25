-- name: ListSearchableEntryIDs :many
SELECT id
FROM entries
WHERE entry_listed(entries)
ORDER BY id;

-- name: CountSearchableDocuments :one
-- How many documents a complete index holds once entry_search_jobs is
-- drained: one per blueprint and planner of a listed entry's current revision
-- (books' children included) plus one per listed entry whose root is a book
-- (the same scope as ListSearchableEntryIDs).
SELECT (
    (SELECT count(*) FROM blueprints b
     JOIN entries e ON e.current_revision_id = b.revision_id
     WHERE entry_listed(e))
    + (SELECT count(*) FROM planners p
       JOIN entries e ON e.current_revision_id = p.revision_id
       WHERE entry_listed(e))
    + (SELECT count(*) FROM entries e
       JOIN revisions r ON r.id = e.current_revision_id
       WHERE entry_listed(e) AND r.tree->>'kind' = 'book')
)::bigint;

-- name: CountSearchJobs :one
-- Every queued, in-flight or failed search intent: while any exists, the index
-- may legitimately differ from PostgreSQL.
SELECT count(*)::bigint
FROM entry_search_jobs;
