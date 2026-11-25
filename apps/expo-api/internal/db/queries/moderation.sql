-- Staff moderation: report queues, staff removals and bans (admin listener
-- only). Every write here runs in the catalog's transaction together with its
-- RecordAdminAction row.

-- name: RecordAdminAction :exec
INSERT INTO admin_actions (id, staff_username, action, target_type, target_id, reason, before, after)
VALUES (
    sqlc.arg(id), sqlc.arg(staff_username), sqlc.arg(action),
    sqlc.arg(target_type), sqlc.arg(target_id), sqlc.arg(reason),
    sqlc.narg(before)::jsonb, sqlc.narg(after)::jsonb
);

-- name: StaffBadges :one
-- The sidebar's counts: reported entries and comments, open problem
-- reports, and jobs that failed for good.
SELECT
    (SELECT count(DISTINCT entry_id) FROM entry_reports WHERE resolved_at IS NULL) AS reported_entries,
    (SELECT count(DISTINCT comment_id) FROM comment_reports WHERE resolved_at IS NULL) AS reported_comments,
    (SELECT count(*) FROM problem_reports WHERE resolved_at IS NULL) AS problem_reports,
    ((SELECT count(*) FROM phase_jobs WHERE state = 'failed')
        + (SELECT count(*) FROM entry_search_jobs WHERE state = 'failed'))::bigint AS failed_jobs;

-- name: ListOpenEntryReports :many
-- Open entry reports, at most entry_limit entries, with the entry as it is
-- now: takedowns (an open "stolen" report) first, then the entries reported
-- longest ago. Deleted and expired entries are included: their reports still
-- need resolving.
WITH reported AS (
    SELECT entry_id, min(created_at) AS first_reported_at, bool_or(reason = 'stolen') AS takedown
    FROM entry_reports
    WHERE resolved_at IS NULL
    GROUP BY entry_id
    ORDER BY takedown DESC, first_reported_at, entry_id
    LIMIT sqlc.arg(entry_limit)::integer
)
SELECT r.id, r.entry_id, r.reason, r.note, r.revision_number, r.title_snapshot, r.description_snapshot,
       r.created_at, coalesce(reporter.username, '')::text AS reporter_username,
       e.title, e.description, e.visibility, e.quick_preview,
       (e.deleted_at IS NOT NULL)::boolean AS deleted,
       (NOT entry_live(e))::boolean AS gone,
       coalesce(owner.username, '')::text AS owner_username,
       coalesce(current.version_number, 0)::integer AS current_revision_number
FROM reported
JOIN entry_reports r ON r.entry_id = reported.entry_id AND r.resolved_at IS NULL
JOIN entries e ON e.id = r.entry_id
LEFT JOIN users reporter ON reporter.id = r.reporter_user_id
LEFT JOIN users owner ON owner.id = e.owner_user_id
LEFT JOIN revisions current ON current.id = e.current_revision_id
ORDER BY reported.takedown DESC, reported.first_reported_at, r.entry_id, r.created_at, r.id;

-- name: ResolveEntryReports :execrows
-- Resolves every open report of an entry; an anonymous reporter's address
-- hash goes with it.
UPDATE entry_reports
SET resolved_at = now(), resolved_by = sqlc.arg(resolved_by), reporter_ip_hash = NULL
WHERE entry_id = sqlc.arg(entry_id) AND resolved_at IS NULL;

-- name: GetOpenEntryReportForUpdate :one
SELECT id, entry_id, reporter_user_id, reporter_ip_hash, reason
FROM entry_reports
WHERE id = $1 AND resolved_at IS NULL
FOR UPDATE;

-- name: ResolveEntryReport :exec
UPDATE entry_reports
SET resolved_at = now(), resolved_by = sqlc.arg(resolved_by), reporter_ip_hash = NULL
WHERE id = sqlc.arg(id) AND resolved_at IS NULL;

-- name: GetEntryForStaffUpdate :one
-- Any entry, deleted and expired ones too, for a staff change.
SELECT * FROM entries WHERE id = $1 FOR UPDATE;

-- name: ListOpenCommentReports :many
-- Open comment reports of the comments reported longest ago (at most
-- comment_limit comments), with the comment and its entry as they are now.
WITH reported AS (
    SELECT comment_id, min(created_at) AS first_reported_at
    FROM comment_reports
    WHERE resolved_at IS NULL
    GROUP BY comment_id
    ORDER BY first_reported_at, comment_id
    LIMIT sqlc.arg(comment_limit)::integer
)
SELECT cr.comment_id, cr.reason, cr.note, cr.body_snapshot, cr.created_at,
       reporter.username AS reporter_username,
       c.entry_id, c.number, c.body, c.deleted_by, c.created_at AS comment_created_at,
       author.username AS author_username,
       e.title AS entry_title
FROM reported
JOIN comment_reports cr ON cr.comment_id = reported.comment_id AND cr.resolved_at IS NULL
JOIN comments c ON c.id = cr.comment_id
JOIN users reporter ON reporter.id = cr.reporter_user_id
JOIN users author ON author.id = c.author_user_id
JOIN entries e ON e.id = c.entry_id
ORDER BY reported.first_reported_at, cr.comment_id, cr.created_at, cr.reporter_user_id;

-- name: ResolveCommentReports :execrows
UPDATE comment_reports
SET resolved_at = now(), resolved_by = sqlc.arg(resolved_by)
WHERE comment_id = sqlc.arg(comment_id) AND resolved_at IS NULL;

-- name: GetCommentByIDForUpdate :one
SELECT * FROM comments WHERE id = $1 FOR UPDATE;

-- name: CreateUserBan :one
INSERT INTO user_bans (id, user_id, actions, reason_code, note, banned_by, expires_at)
VALUES (
    sqlc.arg(id), sqlc.arg(user_id), sqlc.arg(actions)::text[], sqlc.arg(reason_code),
    sqlc.arg(note), sqlc.arg(banned_by), sqlc.narg(expires_at)
)
RETURNING *;

-- name: GetUserBanForUpdate :one
SELECT * FROM user_bans WHERE id = $1 FOR UPDATE;

-- name: SetUserBanEnd :one
UPDATE user_bans SET expires_at = sqlc.narg(expires_at) WHERE id = sqlc.arg(id) RETURNING *;

-- name: RevokeUserBan :one
UPDATE user_bans SET revoked_at = now(), revoked_by = sqlc.arg(revoked_by)
WHERE id = sqlc.arg(id) RETURNING *;

-- name: ListUserBans :many
-- User bans newest first: the active ones, or those that ended (revoked or
-- expired), optionally of one user (case-insensitive username).
SELECT b.*, u.username
FROM user_bans b
JOIN users u ON u.id = b.user_id
WHERE (sqlc.arg(username)::text = '' OR lower(u.username) = lower(sqlc.arg(username)::text))
  AND ban_active(b) = sqlc.arg(active)::boolean
ORDER BY b.created_at DESC, b.id DESC
LIMIT sqlc.arg(row_limit)::integer;

-- name: ListActiveIPBans :many
SELECT ip_hash, action, reason_code, created_at, expires_at
FROM ip_bans
WHERE expires_at > now()
ORDER BY expires_at DESC, ip_hash, action
LIMIT sqlc.arg(row_limit)::integer;

-- name: LiftIPBan :execrows
-- Ends an address ban early: every action, or just one.
DELETE FROM ip_bans
WHERE ip_hash = sqlc.arg(ip_hash)::bytea
  AND (sqlc.arg(action)::text = '' OR action = sqlc.arg(action)::text);
