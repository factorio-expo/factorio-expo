-- Staff console reads (served on the admin listener only).

-- name: AdminOverview :one
-- The Overview page's numbers. Uploads leave quick previews out; "today"
-- starts at midnight UTC, "week" is the last seven days.
SELECT
    (SELECT count(*) FROM entries
     WHERE NOT quick_preview AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')
        AS uploads_today,
    (SELECT count(*) FROM entries
     WHERE NOT quick_preview AND created_at >= now() - interval '7 days')
        AS uploads_week,
    (SELECT count(DISTINCT entry_id) FROM entry_reports WHERE resolved_at IS NULL)
        AS reported_entries,
    (SELECT count(*) FROM entry_reports WHERE resolved_at IS NULL)
        AS open_entry_reports,
    (SELECT count(DISTINCT comment_id) FROM comment_reports WHERE resolved_at IS NULL)
        AS reported_comments,
    (SELECT count(*) FROM comment_reports WHERE resolved_at IS NULL)
        AS open_comment_reports,
    (SELECT count(*) FROM phase_jobs WHERE phase = 'preview' AND state = 'pending') AS pending_previews,
    (SELECT count(*) FROM phase_jobs WHERE phase = 'preview' AND state = 'failed') AS failed_previews,
    (SELECT count(*) FROM phase_jobs WHERE phase = 'analysis' AND state = 'pending') AS pending_analyses,
    (SELECT count(*) FROM phase_jobs WHERE phase = 'analysis' AND state = 'failed') AS failed_analyses,
    (SELECT count(*) FROM entry_search_jobs WHERE state = 'pending') AS pending_search_syncs,
    (SELECT count(*) FROM entry_search_jobs WHERE state = 'failed') AS failed_search_syncs;

-- name: ListAdminActions :many
-- The audit log newest first, one page ending before a (created_at, id)
-- cursor; a null cursor starts at the newest.
SELECT id, staff_username, action, target_type, target_id, reason, before, after, created_at
FROM admin_actions
WHERE sqlc.narg(before_created_at)::timestamptz IS NULL
   OR (created_at, id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.arg(before_id)::text)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: ListFailedJobs :many
-- Preview, analysis and search jobs that failed for good, newest failure
-- first, optionally only one kind or one job (kind and target: the
-- blueprint for preview and analysis, the entry for search). entry_live is
-- false for a deleted or expired entry, whose jobs the dispatchers skip.
SELECT kind, target_id, entry_id, entry_title, entry_live, blueprint_label,
       search_action, failure_code, failure_detail, retry_count, failed_at
FROM (
    SELECT j.phase::text AS kind, j.blueprint_id::text AS target_id, e.id::text AS entry_id,
           e.title AS entry_title, entry_live(e) AS entry_live, b.label AS blueprint_label,
           ''::text AS search_action, j.failure_code, j.failure_detail, j.retry_count,
           j.updated_at AS failed_at
    FROM phase_jobs j
    JOIN blueprints b ON b.id = j.blueprint_id
    JOIN revisions r ON r.id = b.revision_id
    JOIN entries e ON e.id = r.entry_id
    WHERE j.state = 'failed'
    UNION ALL
    SELECT 'search', j.entry_id::text, e.id::text, e.title, entry_live(e), '',
           j.action, j.failure_code, j.failure_detail, j.retry_count, j.updated_at
    FROM entry_search_jobs j
    JOIN entries e ON e.id = j.entry_id
    WHERE j.state = 'failed'
) jobs
WHERE (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind)::text)
  AND (sqlc.narg(target_id)::text IS NULL OR target_id = sqlc.narg(target_id)::text)
ORDER BY failed_at DESC, kind, target_id
LIMIT sqlc.arg(row_limit)::integer;
