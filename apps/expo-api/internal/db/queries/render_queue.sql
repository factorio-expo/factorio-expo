-- Render queue admission and result reuse (catalog/reuse.go,
-- internal/renderqueue). Ordering is ClaimDispatchJobs (phase_jobs.sql).

-- name: LockQueueKey :exec
-- Serialises admissions of one queue key until the transaction ends, so two
-- concurrent uploads by the same uploader cannot both pass the cap.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(queue_key)::text, 0));

-- name: CountWaitingLeaves :one
-- Leaves of live entries with a pending preview or analysis intent under
-- queue_key: what the per-uploader cap counts. Failed intents do not count.
SELECT count(*)::bigint
FROM (
    SELECT DISTINCT blueprint_id FROM phase_jobs
    WHERE state = 'pending' AND queue_key = sqlc.arg(queue_key)
) waiting
JOIN blueprints b ON b.id = waiting.blueprint_id
JOIN revisions r ON r.id = b.revision_id
JOIN entries e ON e.id = r.entry_id
WHERE entry_live(e);

-- name: LastOrderAt :one
-- The latest place of queue_key's waiting leaves (null when none): the next
-- leaf of the key is placed a gap after it (renderqueue.Place).
SELECT max(waiting.order_at)::timestamptz
FROM (
    SELECT blueprint_id, order_at FROM phase_jobs
    WHERE state = 'pending' AND queue_key = sqlc.arg(queue_key)
) waiting
JOIN blueprints b ON b.id = waiting.blueprint_id
JOIN revisions r ON r.id = b.revision_id
JOIN entries e ON e.id = r.entry_id
WHERE entry_live(e);

-- name: ReusePreviews :many
-- Gives waiting leaves (a pending preview intent with no runner job) of
-- revision_id, or of every live entry when it is null, the stored preview of
-- another leaf with the same exchange string, from any entry, and removes
-- their intents. Only a source whose full file is stored (or that needs none)
-- qualifies, so a re-render of an evicted full file is never "satisfied" by
-- the same missing file. The source row is locked until commit: a delete of
-- it waits, so the file sweep never sees the shared files unreferenced.
WITH targets AS (
    SELECT b.id, b.exchange_sha256, b.exchange_string, r.entry_id
    FROM phase_jobs pj
    JOIN blueprints b ON b.id = pj.blueprint_id
    JOIN revisions r ON r.id = b.revision_id
    JOIN entries e ON e.id = r.entry_id
    WHERE pj.phase = 'preview' AND pj.state = 'pending' AND pj.runner_job_id IS NULL AND entry_live(e)
      AND (sqlc.narg(revision_id)::text IS NULL OR b.revision_id = sqlc.narg(revision_id)::text)
), reused AS (
    INSERT INTO previews (blueprint_id, source_sha256, width, height)
    SELECT t.id, src.source_sha256, src.width, src.height
    FROM targets t
    CROSS JOIN LATERAL (
        SELECT p.source_sha256, p.width, p.height
        FROM blueprints sb
        JOIN previews p ON p.blueprint_id = sb.id
        JOIN preview_files f ON f.source_sha256 = p.source_sha256
        WHERE sb.exchange_sha256 = t.exchange_sha256
          AND sb.exchange_string = t.exchange_string
          AND sb.id <> t.id
          AND (f.full_bytes IS NOT NULL
               OR (p.width <= sqlc.arg(thumbnail_max)::int AND p.height <= sqlc.arg(thumbnail_max)::int))
        LIMIT 1
        FOR SHARE OF p
    ) src
    ON CONFLICT (blueprint_id) DO UPDATE
    SET source_sha256 = EXCLUDED.source_sha256, width = EXCLUDED.width, height = EXCLUDED.height
    RETURNING blueprint_id
), done AS (
    DELETE FROM phase_jobs
    WHERE phase = 'preview' AND blueprint_id IN (SELECT blueprint_id FROM reused)
    RETURNING blueprint_id
)
SELECT done.blueprint_id, t.entry_id
FROM done
JOIN targets t ON t.id = done.blueprint_id;

-- name: ReuseProduction :many
-- ReusePreviews for the analysis: copies a current (schema_version, not
-- stale) production snapshot of another leaf with the same exchange string.
WITH targets AS (
    SELECT b.id, b.exchange_sha256, b.exchange_string, r.entry_id
    FROM phase_jobs pj
    JOIN blueprints b ON b.id = pj.blueprint_id
    JOIN revisions r ON r.id = b.revision_id
    JOIN entries e ON e.id = r.entry_id
    WHERE pj.phase = 'analysis' AND pj.state = 'pending' AND pj.runner_job_id IS NULL AND entry_live(e)
      AND (sqlc.narg(revision_id)::text IS NULL OR b.revision_id = sqlc.narg(revision_id)::text)
), stored AS (
    INSERT INTO blueprint_stats (
        blueprint_id, production, production_schema_version, production_state,
        production_stale, production_calculated_at
    )
    SELECT t.id, src.production, src.production_schema_version, src.production_state,
        false, src.production_calculated_at
    FROM targets t
    CROSS JOIN LATERAL (
        SELECT bs.production, bs.production_schema_version, bs.production_state, bs.production_calculated_at
        FROM blueprints sb
        JOIN blueprint_stats bs ON bs.blueprint_id = sb.id
        WHERE sb.exchange_sha256 = t.exchange_sha256
          AND sb.exchange_string = t.exchange_string
          AND sb.id <> t.id
          AND bs.production_schema_version = sqlc.arg(schema_version)::int
          AND bs.production_state IS NOT NULL
          AND NOT bs.production_stale
        LIMIT 1
    ) src
    ON CONFLICT (blueprint_id) DO UPDATE SET
        production = EXCLUDED.production,
        production_schema_version = EXCLUDED.production_schema_version,
        production_state = EXCLUDED.production_state,
        production_stale = false,
        production_calculated_at = EXCLUDED.production_calculated_at
    RETURNING blueprint_id
), done AS (
    DELETE FROM phase_jobs
    WHERE phase = 'analysis' AND blueprint_id IN (SELECT blueprint_id FROM stored)
    RETURNING blueprint_id
)
SELECT done.blueprint_id, t.entry_id
FROM done
JOIN targets t ON t.id = done.blueprint_id;

-- name: ReusePreviewFailures :many
-- Fails waiting preview intents (scoped like ReusePreviews) whose exchange
-- string already failed for good with one of codes (failures that are the
-- blueprint's own, so another run ends the same way) on a leaf with no stored
-- preview.
WITH targets AS (
    SELECT b.id, b.exchange_sha256, b.exchange_string, r.entry_id
    FROM phase_jobs pj
    JOIN blueprints b ON b.id = pj.blueprint_id
    JOIN revisions r ON r.id = b.revision_id
    JOIN entries e ON e.id = r.entry_id
    WHERE pj.phase = 'preview' AND pj.state = 'pending' AND pj.runner_job_id IS NULL AND entry_live(e)
      AND (sqlc.narg(revision_id)::text IS NULL OR b.revision_id = sqlc.narg(revision_id)::text)
), src AS (
    SELECT t.id, t.entry_id, f.failure_code, f.failure_detail
    FROM targets t
    CROSS JOIN LATERAL (
        SELECT sj.failure_code, sj.failure_detail
        FROM blueprints sb
        JOIN phase_jobs sj ON sj.blueprint_id = sb.id AND sj.phase = 'preview'
        WHERE sb.exchange_sha256 = t.exchange_sha256
          AND sb.exchange_string = t.exchange_string
          AND sb.id <> t.id
          AND sj.state = 'failed'
          AND sj.failure_code = ANY (sqlc.arg(codes)::text[])
          AND NOT EXISTS (SELECT 1 FROM previews p WHERE p.blueprint_id = sb.id)
        LIMIT 1
    ) f
)
UPDATE phase_jobs
SET state = 'failed', failure_code = src.failure_code, failure_detail = src.failure_detail, updated_at = now()
FROM src
WHERE phase_jobs.phase = 'preview' AND phase_jobs.blueprint_id = src.id
RETURNING phase_jobs.blueprint_id, src.entry_id, src.failure_code;

-- name: ReuseAnalysisFailures :many
-- ReusePreviewFailures for the analysis phase.
WITH targets AS (
    SELECT b.id, b.exchange_sha256, b.exchange_string, r.entry_id
    FROM phase_jobs pj
    JOIN blueprints b ON b.id = pj.blueprint_id
    JOIN revisions r ON r.id = b.revision_id
    JOIN entries e ON e.id = r.entry_id
    WHERE pj.phase = 'analysis' AND pj.state = 'pending' AND pj.runner_job_id IS NULL AND entry_live(e)
      AND (sqlc.narg(revision_id)::text IS NULL OR b.revision_id = sqlc.narg(revision_id)::text)
), src AS (
    SELECT t.id, t.entry_id, f.failure_code, f.failure_detail
    FROM targets t
    CROSS JOIN LATERAL (
        SELECT sj.failure_code, sj.failure_detail
        FROM blueprints sb
        JOIN phase_jobs sj ON sj.blueprint_id = sb.id AND sj.phase = 'analysis'
        WHERE sb.exchange_sha256 = t.exchange_sha256
          AND sb.exchange_string = t.exchange_string
          AND sb.id <> t.id
          AND sj.state = 'failed'
          AND sj.failure_code = ANY (sqlc.arg(codes)::text[])
        LIMIT 1
    ) f
)
UPDATE phase_jobs
SET state = 'failed', failure_code = src.failure_code, failure_detail = src.failure_detail, updated_at = now()
FROM src
WHERE phase_jobs.phase = 'analysis' AND phase_jobs.blueprint_id = src.id
RETURNING phase_jobs.blueprint_id, src.entry_id, src.failure_code;
