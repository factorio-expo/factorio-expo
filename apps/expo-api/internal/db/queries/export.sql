-- The account data export (GET /api/v1/me/export, internal/dataexport): what the
-- catalog holds about one account. Large lists are read a page at a time and
-- exchange strings one revision at a time, so the export streams.

-- name: ExportAccount :one
SELECT id, username, created_at, status FROM users WHERE id = $1 AND deleted_at IS NULL;

-- name: ExportSocialLinks :many
SELECT platform, handle FROM user_social_links WHERE user_id = $1 ORDER BY platform;

-- name: ExportPinnedEntries :many
-- Every pin, also of entries no longer listed (the rows are still held).
SELECT entry_id, position, pinned_at
FROM user_pinned_entries
WHERE user_id = $1
ORDER BY position, pinned_at;

-- name: ExportIdentities :many
SELECT provider, provider_user_id, created_at, last_used_at
FROM auth_identities
WHERE user_id = $1
ORDER BY provider;

-- name: ExportPasskeys :many
-- Ids and dates only, never the credential (public key, sign count).
SELECT id, created_at, last_used_at
FROM user_passkeys
WHERE user_id = $1
ORDER BY created_at, id;

-- name: ExportSessions :many
SELECT created_at, last_used_at, expires_at, auth_time
FROM user_sessions
WHERE user_id = $1
ORDER BY created_at, id;

-- name: ExportEntries :many
-- The account's entries after a (created_at, id) cursor, oldest first,
-- deleted and expired ones included (they are still held).
SELECT
    e.id, e.title, e.description, e.visibility, e.quick_preview, e.spoiler, e.created_at, e.updated_at,
    e.expires_at, e.deleted_at, e.source_url,
    entry_aliases.slug AS alias,
    coalesce((SELECT array_agg(t.slug ORDER BY t.slug)
              FROM entry_tags et JOIN tags t ON t.id = et.tag_id
              WHERE et.entry_id = e.id), '{}')::text[] AS tags
FROM entries e
LEFT JOIN entry_aliases ON entry_aliases.entry_id = e.id
WHERE e.owner_user_id = sqlc.arg(user_id)::text
  AND (e.created_at, e.id) > (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::text)
ORDER BY e.created_at, e.id
LIMIT sqlc.arg(row_limit)::integer;

-- name: ExportRevisions :many
-- An entry's revisions without their strings (ExportRevisionString).
SELECT id, version_number, game_version, note, created_at
FROM revisions
WHERE entry_id = $1
ORDER BY version_number;

-- name: ExportRevisionString :one
SELECT original_string FROM revisions WHERE id = $1;

-- name: ExportComments :many
-- The account's comments after a (created_at, id) cursor, oldest first,
-- removed ones (tombstones) included.
SELECT c.id, c.entry_id, c.number, c.revision_number, c.body, c.created_at, c.edited_at,
       c.deleted_at, c.deleted_by
FROM comments c
WHERE c.author_user_id = sqlc.arg(user_id)::text
  AND (c.created_at, c.id) > (sqlc.arg(after_created_at)::timestamptz, sqlc.arg(after_id)::text)
ORDER BY c.created_at, c.id
LIMIT sqlc.arg(row_limit)::integer;

-- name: ExportFavorites :many
SELECT entry_id, created_at
FROM favorites
WHERE user_id = $1
ORDER BY created_at, entry_id;

-- name: ExportCollections :many
SELECT id, name, description, visibility, created_at
FROM collections
WHERE user_id = $1
ORDER BY created_at, id;

-- name: ExportCollectionEntries :many
SELECT entry_id, position, added_at
FROM collection_entries
WHERE collection_id = $1
ORDER BY position, added_at, entry_id;

-- name: ExportEntryReports :many
-- Entry reports the account filed. The snapshots are other people's text.
SELECT id, entry_id, reason, note, revision_number, created_at, resolved_at
FROM entry_reports
WHERE reporter_user_id = $1
ORDER BY created_at, id;

-- name: ExportCommentReports :many
-- Comment reports the account filed. The body snapshot is someone else's text.
SELECT c.entry_id, c.number AS comment_number, r.reason, r.note, r.created_at, r.resolved_at
FROM comment_reports r
JOIN comments c ON c.id = r.comment_id
WHERE r.reporter_user_id = $1
ORDER BY r.created_at, c.entry_id, c.number;

-- name: ExportXPEvents :many
-- The account's XP ledger, every row (also those that no longer count).
SELECT reason, points, entry_id, comment_id, challenge_id, board, created_at
FROM xp_events
WHERE user_id = $1
ORDER BY created_at, id;
