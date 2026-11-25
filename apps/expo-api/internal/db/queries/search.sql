-- name: EnqueueSearchIndexJob :exec
-- Marks an entry as needing a (re)index and clears any prior failure. A
-- queued 'delete' is never overwritten: deleted entries are not undeleted, and
-- a late import must not turn the removal back into a failing index job.
INSERT INTO entry_search_jobs (entry_id, action)
VALUES ($1, 'index')
ON CONFLICT (entry_id) DO UPDATE
    SET action         = 'index',
        state          = 'pending',
        retry_count    = 0,
        failure_code   = '',
        failure_detail = '',
        available_at   = now(),
        updated_at     = now()
WHERE entry_search_jobs.action <> 'delete';

-- name: EnqueueSearchDeleteJob :exec
-- Marks an entry as needing removal from the search index, overriding any
-- pending index job (a delete always wins over a stale index intent).
INSERT INTO entry_search_jobs (entry_id, action)
VALUES ($1, 'delete')
ON CONFLICT (entry_id) DO UPDATE
    SET action         = 'delete',
        state          = 'pending',
        retry_count    = 0,
        failure_code   = '',
        failure_detail = '',
        available_at   = now(),
        updated_at     = now();

-- name: EnqueueSearchResyncJob :exec
-- A staff sync of the entry's search document: 'index' indexes a listed
-- entry and removes any other, so unlike EnqueueSearchIndexJob it may replace
-- a queued removal (a restore, or a reindex of a deleted entry).
INSERT INTO entry_search_jobs (entry_id, action)
VALUES ($1, 'index')
ON CONFLICT (entry_id) DO UPDATE
    SET action         = 'index',
        state          = 'pending',
        retry_count    = 0,
        failure_code   = '',
        failure_detail = '',
        available_at   = now(),
        updated_at     = now();

-- name: EnqueueSearchIndexJobsForTag :many
-- Before a tag is deleted: its entries' search documents lose it. A queued
-- removal is never overwritten. Returns the entries queued.
INSERT INTO entry_search_jobs (entry_id, action)
SELECT et.entry_id, 'index' FROM entry_tags et WHERE et.tag_id = $1
ON CONFLICT (entry_id) DO UPDATE
    SET action         = 'index',
        state          = 'pending',
        retry_count    = 0,
        failure_code   = '',
        failure_detail = '',
        available_at   = now(),
        updated_at     = now()
WHERE entry_search_jobs.action <> 'delete'
RETURNING entry_search_jobs.entry_id;

-- name: ClaimSearchJobs :many
-- Leases due rows (FOR UPDATE SKIP LOCKED, available_at pushed out by the
-- lease) so concurrent claimers never receive the same entry twice. Claiming
-- leaves updated_at alone and every enqueue sets it, so the resolutions below
-- take the claimed row's updated_at (claimed_updated_at) and do nothing when
-- the entry was re-enqueued (deleted, edited, made private) while its job was
-- in flight: the newer intent is never deleted or overwritten.
UPDATE entry_search_jobs
SET available_at = now() + make_interval(secs => sqlc.arg(lease_seconds)::float8)
WHERE entry_id IN (
    SELECT entry_id
    FROM entry_search_jobs
    WHERE state = 'pending' AND available_at <= now()
    ORDER BY available_at, created_at
    LIMIT sqlc.arg(batch_size)
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ReleaseSearchJob :exec
-- Makes a claimed row due again without spending retry budget.
UPDATE entry_search_jobs
SET available_at = sqlc.arg(available_at),
    updated_at   = now()
WHERE entry_id = sqlc.arg(entry_id)
  AND updated_at = sqlc.arg(claimed_updated_at)
  AND state = 'pending';

-- name: RetrySearchJob :exec
UPDATE entry_search_jobs
SET retry_count    = retry_count + 1,
    failure_code   = sqlc.arg(failure_code),
    failure_detail = sqlc.arg(failure_detail),
    available_at   = sqlc.arg(available_at),
    updated_at     = now()
WHERE entry_id = sqlc.arg(entry_id)
  AND updated_at = sqlc.arg(claimed_updated_at);

-- name: MarkSearchJobFailed :exec
UPDATE entry_search_jobs
SET state          = 'failed',
    failure_code   = sqlc.arg(failure_code),
    failure_detail = sqlc.arg(failure_detail),
    updated_at     = now()
WHERE entry_id = sqlc.arg(entry_id)
  AND updated_at = sqlc.arg(claimed_updated_at);

-- name: DeleteSearchJob :exec
DELETE
FROM entry_search_jobs
WHERE entry_id = sqlc.arg(entry_id)
  AND updated_at = sqlc.arg(claimed_updated_at);

-- name: RetryFailedSearchJob :execrows
-- Queues a search job that failed for good again, with a fresh retry budget
-- (a staff re-run). An open or missing job is left alone.
UPDATE entry_search_jobs
SET state          = 'pending',
    retry_count    = 0,
    failure_code   = '',
    failure_detail = '',
    available_at   = now(),
    updated_at     = now()
WHERE entry_id = $1 AND state = 'failed';
