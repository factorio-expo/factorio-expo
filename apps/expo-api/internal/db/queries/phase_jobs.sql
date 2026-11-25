-- name: EnqueuePhaseJob :exec
-- queue_key and order_at place the intent in the render queue
-- (internal/renderqueue).
INSERT INTO phase_jobs (blueprint_id, phase, queue_key, order_at)
VALUES (sqlc.arg(blueprint_id), sqlc.arg(phase), sqlc.arg(queue_key), sqlc.arg(order_at))
ON CONFLICT (blueprint_id, phase) DO NOTHING;

-- name: RequeuePhaseJob :one
-- Queues the phase again (preview.Requeuer): adds the intent, or with
-- retry_failed resets one that failed for good. Returns the entry only when
-- something was queued, so an open intent (or a failed one without
-- retry_failed) gives no rows.
WITH queued AS (
    INSERT INTO phase_jobs (blueprint_id, phase, queue_key, order_at)
    VALUES (sqlc.arg(blueprint_id), sqlc.arg(phase), sqlc.arg(queue_key), sqlc.arg(order_at))
    ON CONFLICT (blueprint_id, phase) DO UPDATE
    SET state = 'pending', runner_job_id = NULL, runner_settled = false, retry_count = 0,
        available_at = now(), queue_key = EXCLUDED.queue_key, order_at = EXCLUDED.order_at,
        failure_code = '', failure_detail = '', updated_at = now()
    WHERE phase_jobs.state = 'failed' AND sqlc.arg(retry_failed)::boolean
    RETURNING blueprint_id
)
SELECT r.entry_id
FROM queued
JOIN blueprints b ON b.id = queued.blueprint_id
JOIN revisions r ON r.id = b.revision_id;

-- name: ReconcileMissingPreviewJobs :exec
-- Safety net for a lost intent: only the live revision of a live entry is
-- reconciled. Intents are written transactionally with the revision, so this
-- runs at startup and on a slow timer, not on every poll. The repaired rows
-- queue under the 'system' key at order_at.
INSERT INTO phase_jobs (blueprint_id, phase, queue_key, order_at)
SELECT blueprints.id, 'preview', 'system', sqlc.arg(order_at)
FROM blueprints
JOIN entries ON entries.current_revision_id = blueprints.revision_id
    AND entry_live(entries)
LEFT JOIN previews ON previews.blueprint_id = blueprints.id
LEFT JOIN phase_jobs ON phase_jobs.blueprint_id = blueprints.id AND phase_jobs.phase = 'preview'
WHERE previews.blueprint_id IS NULL AND phase_jobs.blueprint_id IS NULL
ON CONFLICT (blueprint_id, phase) DO NOTHING;

-- name: ClaimDispatchJobs :many
-- Leases due preview and analysis rows of live entries (FOR UPDATE SKIP
-- LOCKED, available_at pushed out by the lease) and returns each blueprint
-- once with both phase rows and which phases were claimed. Other claimers,
-- and a crashed poll after the lease, never see the same row twice.
--
-- Rows that already have a runner job are always claimed (to be polled).
-- Rows still to be sent are claimed only for the leaves with the earliest
-- place (order_at, fixed when queued: renderqueue.Place) that fit the
-- dispatch window: dispatch_window minus the runner jobs not yet settled. A
-- leaf whose exchange string is already in the runner waits; the dispatcher
-- copies the result to it once that one is stored (catalog reuse).
WITH waiting AS (
    SELECT blueprint_id, order_at, runner_job_id, runner_settled, available_at
    FROM phase_jobs WHERE state = 'pending'
), leaves AS (
    SELECT
        w.blueprint_id,
        b.exchange_sha256,
        min(w.order_at) AS score,
        bool_or(w.runner_job_id IS NULL AND w.available_at <= now()) AS sendable,
        bool_or(w.runner_job_id IS NOT NULL) AS sent,
        count(DISTINCT w.runner_job_id) FILTER (WHERE w.runner_job_id IS NOT NULL AND NOT w.runner_settled) AS running
    FROM waiting w
    JOIN blueprints b ON b.id = w.blueprint_id
    JOIN revisions r ON r.id = b.revision_id
    JOIN entries e ON e.id = r.entry_id
    WHERE entry_live(e)
    GROUP BY w.blueprint_id, b.exchange_sha256
), candidates AS (
    SELECT DISTINCT ON (s.exchange_sha256) s.blueprint_id, s.score
    FROM leaves s
    WHERE s.sendable
      AND s.exchange_sha256 NOT IN (SELECT exchange_sha256 FROM leaves WHERE sent)
    ORDER BY s.exchange_sha256, s.score, s.blueprint_id
), picked AS (
    SELECT ranked.blueprint_id
    FROM (
        SELECT c.blueprint_id, row_number() OVER (ORDER BY c.score, c.blueprint_id) AS place
        FROM candidates c
    ) ranked
    WHERE ranked.place <= sqlc.arg(dispatch_window)::bigint - (SELECT COALESCE(sum(running), 0) FROM leaves)
), claimed AS (
    UPDATE phase_jobs
    SET available_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::float8)
    WHERE (blueprint_id, phase) IN (
        SELECT pj.blueprint_id, pj.phase
        FROM phase_jobs pj
        JOIN blueprints b ON b.id = pj.blueprint_id
        JOIN revisions r ON r.id = b.revision_id
        JOIN entries e ON e.id = r.entry_id
        WHERE pj.state = 'pending' AND pj.available_at <= now() AND entry_live(e)
          AND (pj.runner_job_id IS NOT NULL OR pj.blueprint_id IN (SELECT blueprint_id FROM picked))
        ORDER BY pj.available_at, pj.created_at
        LIMIT sqlc.arg(batch_size)
        FOR UPDATE OF pj SKIP LOCKED
    )
    RETURNING blueprint_id, phase
)
SELECT
    blueprints.id AS blueprint_id,
    revisions.entry_id,
    (claimed_preview.blueprint_id IS NOT NULL)::boolean AS preview_claimed,
    (claimed_analysis.blueprint_id IS NOT NULL)::boolean AS analysis_claimed,
    preview_jobs.runner_job_id AS preview_runner_job_id,
    preview_jobs.state AS preview_state,
    preview_jobs.retry_count AS preview_retry_count,
    analysis_jobs.runner_job_id AS analysis_runner_job_id,
    analysis_jobs.state AS analysis_state,
    analysis_jobs.retry_count AS analysis_retry_count,
    blueprints.exchange_string
FROM blueprints
JOIN revisions ON revisions.id = blueprints.revision_id
LEFT JOIN claimed claimed_preview ON claimed_preview.blueprint_id = blueprints.id AND claimed_preview.phase = 'preview'
LEFT JOIN claimed claimed_analysis ON claimed_analysis.blueprint_id = blueprints.id AND claimed_analysis.phase = 'analysis'
LEFT JOIN phase_jobs preview_jobs ON preview_jobs.blueprint_id = blueprints.id AND preview_jobs.phase = 'preview'
LEFT JOIN phase_jobs analysis_jobs ON analysis_jobs.blueprint_id = blueprints.id AND analysis_jobs.phase = 'analysis'
WHERE claimed_preview.blueprint_id IS NOT NULL OR claimed_analysis.blueprint_id IS NOT NULL;

-- name: AssignRunnerPhaseJob :exec
UPDATE phase_jobs
SET runner_job_id = sqlc.arg(runner_job_id),
    runner_settled = false,
    failure_code = '',
    failure_detail = '',
    available_at = now(),
    updated_at = now()
WHERE blueprint_id = sqlc.arg(blueprint_id) AND phase = sqlc.arg(phase)
  AND state = 'pending' AND runner_job_id IS NULL;

-- name: SettleRunnerJob :exec
-- The runner job finished: its phases no longer hold a dispatch window slot
-- while their results are imported.
UPDATE phase_jobs SET runner_settled = true
WHERE runner_job_id = sqlc.arg(runner_job_id) AND NOT runner_settled;

-- name: RetryPhaseJob :exec
UPDATE phase_jobs
SET runner_job_id  = NULL,
    runner_settled = false,
    state          = 'pending',
    retry_count    = retry_count + 1,
    failure_code   = sqlc.arg(failure_code),
    failure_detail = sqlc.arg(failure_detail),
    available_at   = sqlc.arg(available_at),
    updated_at     = now()
WHERE blueprint_id = sqlc.arg(blueprint_id) AND phase = sqlc.arg(phase);

-- name: ReleasePhaseJob :exec
-- Makes a claimed row due again at available_at without spending retry budget.
UPDATE phase_jobs
SET available_at = sqlc.arg(available_at),
    updated_at   = now()
WHERE blueprint_id = sqlc.arg(blueprint_id) AND phase = sqlc.arg(phase) AND state = 'pending';

-- name: MarkPhaseJobFailed :exec
UPDATE phase_jobs
SET state          = 'failed',
    failure_code   = sqlc.arg(failure_code),
    failure_detail = sqlc.arg(failure_detail),
    updated_at     = now()
WHERE blueprint_id = sqlc.arg(blueprint_id) AND phase = sqlc.arg(phase);

-- name: DeletePhaseJob :exec
DELETE FROM phase_jobs WHERE blueprint_id = sqlc.arg(blueprint_id) AND phase = sqlc.arg(phase);
