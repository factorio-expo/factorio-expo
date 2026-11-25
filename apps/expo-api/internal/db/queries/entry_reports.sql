-- name: CreateEntryReport :execrows
-- At most one open report per reporter (account, or address hash when
-- anonymous) per entry; reporting again while it is open changes nothing.
INSERT INTO entry_reports (
    id, entry_id, reporter_user_id, reporter_ip_hash, reason, note,
    revision_number, title_snapshot, description_snapshot
)
VALUES (
    sqlc.arg(id), sqlc.arg(entry_id), sqlc.narg(reporter_user_id), sqlc.narg(reporter_ip_hash),
    sqlc.arg(reason), sqlc.arg(note), sqlc.arg(revision_number),
    sqlc.arg(title_snapshot), sqlc.arg(description_snapshot)
)
ON CONFLICT DO NOTHING;
