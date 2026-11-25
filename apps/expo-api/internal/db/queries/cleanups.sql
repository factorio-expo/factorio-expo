-- Staff cleanups (System -> Cleanups): what each would remove, and the
-- removals the automatic sweeps do not do.

-- name: CountSweepableDeletedEntries :one
-- Soft-deleted entries whose search removal has finished: the ones a hard
-- delete may remove (a pending removal still needs the row).
SELECT count(*)
FROM entries e
WHERE e.deleted_at IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM entry_search_jobs j WHERE j.entry_id = e.id);

-- name: HardDeleteDeletedEntries :many
-- Hard-deletes a batch of them. The cascade removes revisions, nodes,
-- previews (their files are queued by trigger), jobs, comments, reports,
-- favorites and collection items.
DELETE FROM entries
WHERE id IN (
    SELECT e.id
    FROM entries e
    WHERE e.deleted_at IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM entry_search_jobs j WHERE j.entry_id = e.id)
    ORDER BY e.deleted_at, e.id
    LIMIT sqlc.arg(row_limit)::integer
)
RETURNING id;

-- name: CountExpiredEntries :one
-- Expired entries the minute sweep has not removed yet.
SELECT count(*)
FROM entries
WHERE deleted_at IS NULL AND expires_at <= now();

-- name: CountExpiredIPBans :one
-- Expired address bans and ban holds: what PruneBanData deletes.
SELECT ((SELECT count(*) FROM ip_bans WHERE expires_at < now())
        + (SELECT count(*) FROM ban_holds WHERE expires_at < now()))::bigint;

-- name: CountStaleModDetections :one
SELECT count(*) FROM blueprint_stats WHERE mods_version <> sqlc.arg(mods_version)::text;

-- name: PreviewFileTotals :one
-- Every recorded preview file's bytes, and the full files' alone.
SELECT coalesce(sum(thumbnail_bytes + coalesce(full_bytes, 0)), 0)::bigint AS total_bytes,
       coalesce(sum(full_bytes), 0)::bigint AS full_bytes,
       count(full_bytes)::bigint AS full_files
FROM preview_files;

-- name: CountEvictionsToTarget :one
-- How many full files, oldest first, must go to bring the recorded total down
-- to target bytes, and the bytes they free.
WITH totals AS (
    SELECT coalesce(sum(thumbnail_bytes + coalesce(full_bytes, 0)), 0)::bigint AS total FROM preview_files
), ordered AS (
    SELECT full_bytes,
           sum(full_bytes) OVER (ORDER BY stored_at, source_sha256) - full_bytes AS freed_before
    FROM preview_files
    WHERE full_bytes IS NOT NULL
)
SELECT count(*)::bigint AS files, coalesce(sum(ordered.full_bytes), 0)::bigint AS bytes
FROM ordered, totals
WHERE ordered.freed_before < totals.total - sqlc.arg(target_bytes)::bigint;

-- name: ListPreviewFileSources :many
-- Recorded preview sources after a cursor, in order.
SELECT source_sha256 FROM preview_files
WHERE source_sha256 > sqlc.arg(after)::text
ORDER BY source_sha256
LIMIT sqlc.arg(row_limit)::integer;

-- name: ListReferencedPreviewSources :many
-- Which of these sources a previews row references.
SELECT DISTINCT source_sha256 FROM previews WHERE source_sha256 = ANY (sqlc.arg(sources)::text[]);
