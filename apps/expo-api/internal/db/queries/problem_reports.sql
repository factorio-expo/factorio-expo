-- Problem reports: the site's bug button, reviewed in the staff console.

-- name: CreateProblemReport :one
INSERT INTO problem_reports (
    id, reporter_user_id, reporter_ip_hash, body, email, page_path, entry_id, blueprint_id, context, user_agent
) VALUES (
    sqlc.arg(id), sqlc.narg(reporter_user_id), sqlc.narg(reporter_ip_hash), sqlc.arg(body), sqlc.narg(email),
    sqlc.arg(page_path), sqlc.narg(entry_id), sqlc.narg(blueprint_id), sqlc.arg(context)::jsonb, sqlc.arg(user_agent)
)
RETURNING id;

-- name: ListProblemReports :many
-- The open queue, oldest first, or the resolved ones (kept 7 days), the
-- latest resolved first; with the reporter's username.
SELECT p.*, coalesce(u.username, '')::text AS reporter_username
FROM problem_reports p
LEFT JOIN users u ON u.id = p.reporter_user_id
WHERE (p.resolved_at IS NULL) = sqlc.arg(open)::boolean
ORDER BY
    CASE WHEN sqlc.arg(open)::boolean THEN p.created_at END ASC,
    p.resolved_at DESC,
    p.id
LIMIT sqlc.arg(row_limit)::integer;

-- name: CountProblemReports :one
SELECT
    count(*) FILTER (WHERE resolved_at IS NULL)::bigint AS open,
    count(*) FILTER (WHERE resolved_at IS NOT NULL)::bigint AS resolved
FROM problem_reports;

-- name: GetProblemReportForUpdate :one
SELECT * FROM problem_reports WHERE id = $1 FOR UPDATE;

-- name: ResolveProblemReport :exec
-- Closes a report; the contact address and an anonymous reporter's address
-- hash go with it.
UPDATE problem_reports
SET status = sqlc.arg(status), staff_note = sqlc.arg(staff_note),
    resolved_at = now(), resolved_by = sqlc.arg(resolved_by),
    email = NULL, reporter_ip_hash = NULL
WHERE id = sqlc.arg(id) AND resolved_at IS NULL;

-- name: SetProblemReportNote :execrows
-- Changes a report's staff note, open or resolved.
UPDATE problem_reports SET staff_note = sqlc.arg(staff_note) WHERE id = sqlc.arg(id);

-- name: DeleteResolvedProblemReports :execrows
-- The hourly sweep: resolved reports go resolved_before (7 days ago).
DELETE FROM problem_reports
WHERE resolved_at < sqlc.arg(resolved_before)::timestamptz;

-- name: ClearStaleProblemReportEmails :execrows
-- The hourly sweep: an open report keeps its contact address for 30 days
-- (sent_before) at most.
UPDATE problem_reports
SET email = NULL
WHERE resolved_at IS NULL AND email IS NOT NULL AND created_at < sqlc.arg(sent_before)::timestamptz;
