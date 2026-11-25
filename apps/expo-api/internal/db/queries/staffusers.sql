-- Staff console -> Users (admin listener only). Deleted accounts are
-- included here; every other users query skips them.

-- name: ListStaffUsers :many
-- One page of accounts, newest first, ending before a (created_at, id)
-- cursor (null starts at the newest). query matches a part of the
-- username, any case; status is all, active, banned or deleted.
SELECT
    u.id, u.username, u.created_at, u.deleted_at,
    (SELECT count(*) FROM entries e WHERE e.owner_user_id = u.id AND e.deleted_at IS NULL)::bigint
        AS entries,
    (SELECT count(*) FROM comments c WHERE c.author_user_id = u.id AND c.deleted_at IS NULL)::bigint
        AS comments,
    coalesce((
        SELECT array_agg(DISTINCT action ORDER BY action)
        FROM user_bans b, unnest(b.actions) AS action
        WHERE b.user_id = u.id AND ban_active(b)
    ), '{}')::text[] AS ban_actions,
    coalesce((SELECT array_agg(i.provider ORDER BY i.provider) FROM auth_identities i WHERE i.user_id = u.id),
             '{}')::text[] AS providers,
    (SELECT count(*) FROM user_passkeys p WHERE p.user_id = u.id)::bigint AS passkeys
FROM users u
WHERE (sqlc.arg(query)::text = ''
       OR strpos(lower(u.username), lower(sqlc.arg(query)::text)) > 0)
  AND CASE sqlc.arg(status)::text
          WHEN 'active' THEN u.deleted_at IS NULL
          WHEN 'deleted' THEN u.deleted_at IS NOT NULL
          WHEN 'banned' THEN EXISTS (
              SELECT 1 FROM user_bans b
              WHERE b.user_id = u.id AND ban_active(b))
          ELSE true
      END
  AND (sqlc.narg(before_created_at)::timestamptz IS NULL
       OR (u.created_at, u.id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.arg(before_id)::text))
ORDER BY u.created_at DESC, u.id DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: StaffUserCounts :one
-- The Users page's filter counts.
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE deleted_at IS NULL)::bigint AS active,
    count(*) FILTER (WHERE EXISTS (
        SELECT 1 FROM user_bans b
        WHERE b.user_id = users.id AND ban_active(b)
    ))::bigint AS banned,
    count(*) FILTER (WHERE deleted_at IS NOT NULL)::bigint AS deleted
FROM users;

-- name: GetStaffUser :one
-- One account with its activity counts.
SELECT
    u.id, u.username, u.created_at, u.deleted_at, u.status,
    (SELECT count(*) FROM entries e WHERE e.owner_user_id = u.id AND e.deleted_at IS NULL)::bigint
        AS entries,
    (SELECT count(*) FROM entries e WHERE e.owner_user_id = u.id AND e.deleted_at IS NOT NULL)::bigint
        AS deleted_entries,
    (SELECT count(*) FROM comments c WHERE c.author_user_id = u.id AND c.deleted_at IS NULL)::bigint
        AS comments,
    (SELECT count(*) FROM favorites f WHERE f.user_id = u.id)::bigint AS favorites,
    (SELECT count(*) FROM collections c WHERE c.user_id = u.id)::bigint AS collections,
    ((SELECT count(*) FROM entry_reports r WHERE r.reporter_user_id = u.id)
     + (SELECT count(*) FROM comment_reports r WHERE r.reporter_user_id = u.id))::bigint AS reports_filed,
    coalesce((SELECT array_agg(i.provider ORDER BY i.provider) FROM auth_identities i WHERE i.user_id = u.id),
             '{}')::text[] AS providers,
    (SELECT count(*) FROM user_passkeys p WHERE p.user_id = u.id)::bigint AS passkeys
FROM users u
WHERE u.id = $1;

-- name: ListStaffUserEntries :many
-- An account's entries, newest first, deleted ones included.
SELECT id, title, visibility, quick_preview, created_at, deleted_at
FROM entries
WHERE owner_user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit)::integer;

-- name: GetUserForStaffUpdate :one
SELECT * FROM users WHERE id = $1 FOR UPDATE;

-- name: MarkUserDeleted :execrows
-- The caller also deletes the account's sessions (DeleteUserSessions) and
-- sign-in methods (DeleteUserIdentities, DeleteUserPasskeys); the recovery
-- code goes with them.
UPDATE users
SET deleted_at = now(), recovery_code_hash = NULL
WHERE id = $1 AND deleted_at IS NULL;

-- name: DeleteUserIdentities :many
-- At account deletion: the provider accounts may sign up anew at once,
-- unless the account is banned (ban_holds; the caller hashes what this
-- returns).
DELETE FROM auth_identities WHERE user_id = $1
RETURNING provider, provider_user_id;

-- name: DeleteUserPasskeys :execrows
DELETE FROM user_passkeys WHERE user_id = $1;

-- name: ListLiveOwnedEntriesForUpdate :many
SELECT id FROM entries
WHERE owner_user_id = $1 AND deleted_at IS NULL
ORDER BY id
FOR UPDATE;

-- name: RemoveUserComments :execrows
-- Tombstones every comment of the account; numbers stay. deleted_by is
-- 'admin' (staff deleted the account) or 'account' (its owner did).
UPDATE comments
SET body = '', deleted_at = now(), deleted_by = sqlc.arg(deleted_by)::text
WHERE author_user_id = sqlc.arg(user_id)::text AND deleted_at IS NULL;

-- name: ResolveUserCommentReports :execrows
-- Resolves the open reports of the account's comments.
UPDATE comment_reports
SET resolved_at = now(), resolved_by = sqlc.arg(resolved_by)::text
WHERE resolved_at IS NULL
  AND comment_id IN (SELECT id FROM comments WHERE author_user_id = sqlc.arg(user_id)::text);

-- name: DeleteUserFavorites :execrows
DELETE FROM favorites WHERE user_id = $1;

-- name: DeleteUserCollections :execrows
-- Their entry lists cascade.
DELETE FROM collections WHERE user_id = $1;

-- name: CountHardDeletableUsers :one
-- Deleted accounts that own no entry any more (hard-delete those first).
SELECT count(*)
FROM users u
WHERE u.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.owner_user_id = u.id);

-- name: HardDeleteDeletedUsers :many
-- Removes them for good; bans, reports filed, comments, favorites and
-- collections cascade, and the username becomes free again (a banned one
-- stays refused through its ban hold). Only accounts deleted before
-- deleted_before.
DELETE FROM users
WHERE id IN (
    SELECT u.id
    FROM users u
    WHERE u.deleted_at < sqlc.arg(deleted_before)::timestamptz
      AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.owner_user_id = u.id)
    ORDER BY u.deleted_at, u.id
    LIMIT sqlc.arg(row_limit)::integer
)
RETURNING id;

-- name: HardDeleteDeletedUserEntries :many
-- The entries of accounts deleted before deleted_before (all soft-deleted
-- with the account) whose search removal has finished, a batch at a time.
-- The cascade is HardDeleteDeletedEntries' (cleanups.sql): revisions, nodes,
-- previews (their files are queued by trigger), jobs, comments, reports,
-- favorites and collection items.
DELETE FROM entries
WHERE id IN (
    SELECT e.id
    FROM entries e
    JOIN users u ON u.id = e.owner_user_id
    WHERE u.deleted_at < sqlc.arg(deleted_before)::timestamptz
      AND e.deleted_at IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM entry_search_jobs j WHERE j.entry_id = e.id)
    ORDER BY e.id
    LIMIT sqlc.arg(row_limit)::integer
)
RETURNING id;

-- name: ListUserBansByUser :many
-- Every ban of one account, newest first.
SELECT * FROM user_bans
WHERE user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT 100;

-- name: ListDeletedAccounts :many
-- The account deletion queue: deleted accounts waiting for their hard
-- delete, the oldest deletion first, with the entries they still own.
SELECT u.id, u.username, u.deleted_at,
       (SELECT count(*) FROM entries e WHERE e.owner_user_id = u.id)::bigint AS entries,
       EXISTS (
           SELECT 1 FROM user_bans b
           WHERE b.user_id = u.id AND ban_active(b)
       )::boolean AS banned
FROM users u
WHERE u.deleted_at IS NOT NULL
ORDER BY u.deleted_at, u.id
LIMIT sqlc.arg(row_limit)::integer;
