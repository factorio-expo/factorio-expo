-- Raw exchange-string reads. They serve anonymous, shared-cacheable
-- responses, so they match only live entries any signed-out visitor may see
-- (public and unlisted, quick previews until they expire), never private ones.
-- Each returns the entry's expires_at so caches never outlive the entry.

-- name: GetLiveRevisionRaw :one
SELECT revisions.id, revisions.original_string, entries.expires_at
FROM entries
JOIN revisions ON revisions.id = entries.current_revision_id
WHERE entries.id = $1
  AND entry_live(entries)
  AND entry_visible_to(entries, NULL);

-- name: GetRevisionRaw :one
SELECT revisions.original_string, entries.expires_at
FROM revisions
JOIN entries ON entries.id = revisions.entry_id
WHERE revisions.id = sqlc.arg(revision_id)
  AND entries.id = sqlc.arg(entry_id)
  AND entry_live(entries)
  AND entry_visible_to(entries, NULL);

-- name: GetBlueprintRaw :one
SELECT blueprints.exchange_string, entries.expires_at
FROM blueprints
JOIN revisions ON revisions.id = blueprints.revision_id
JOIN entries ON entries.id = revisions.entry_id
WHERE blueprints.id = $1
  AND entry_live(entries)
  AND entry_visible_to(entries, NULL);
