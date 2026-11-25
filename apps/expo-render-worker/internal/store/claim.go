package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

const claimLockKey int64 = 0x66786578706f01

// ClaimNext fails the running jobs whose lease expired (the worker crashed,
// hung or restarted mid-job: Open expires every lease) and hands out the oldest
// queued job. Each job runs once; the API retries a failure it reports as
// retryable with a new job. The failed ids are returned as interrupted once
// that is committed, even when no job is claimed, so the caller can remove
// their inboxes.
func (s *Store) ClaimNext(ctx context.Context, workerID string, lease time.Duration) (
	job jobs.Job, claimed bool, interrupted []string, err error,
) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return jobs.Job{}, false, nil, fmt.Errorf("acquire claim connection: %w", err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, false, nil, fmt.Errorf("begin job claim: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, claimLockKey); err != nil {
		return jobs.Job{}, false, nil, fmt.Errorf("lock job claim: %w", err)
	}

	now := time.Now().UTC()
	failed, err := failInterruptedTx(ctx, tx, now)
	if err != nil {
		return jobs.Job{}, false, nil, err
	}
	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM runner_jobs
		WHERE state = 'queued'
		  AND available_at <= $1
		  AND NOT EXISTS (SELECT 1 FROM runner_jobs WHERE state = 'running')
		ORDER BY created_at, id
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return jobs.Job{}, false, nil, fmt.Errorf("commit empty claim: %w", err)
		}
		committed = true
		return jobs.Job{}, false, failed, nil
	}
	if err != nil {
		return jobs.Job{}, false, nil, fmt.Errorf("select queued job: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE runner_jobs
		SET state = 'running', claimed_by = $1, lease_expires_at = $2,
		    failure_code = '', failure_detail = '', updated_at = $3
		WHERE id = $4 AND state = 'queued'`,
		workerID, now.Add(lease), now, id)
	if err != nil {
		return jobs.Job{}, false, nil, fmt.Errorf("claim job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return jobs.Job{}, false, nil, fmt.Errorf("claim job affected %d rows: %w", affected, err)
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, false, nil, fmt.Errorf("commit claim: %w", err)
	}
	committed = true
	job, err = s.Get(ctx, id)
	return job, err == nil, failed, err
}

// failInterruptedTx fails every running job whose lease expired, with its
// pending phases, as jobs.InterruptedCode (retryable), and returns their ids.
func failInterruptedTx(ctx context.Context, tx *sql.Tx, now time.Time) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		UPDATE runner_jobs SET state = 'failed', claimed_by = NULL, lease_expires_at = NULL,
			failure_code = $1, failure_detail = $2, updated_at = $3,
			preview_status = CASE WHEN requested_preview AND preview_status = 'pending' THEN 'failed' ELSE preview_status END,
			preview_failure_code = CASE WHEN requested_preview AND preview_status = 'pending' THEN $1 ELSE preview_failure_code END,
			preview_failure_detail = CASE WHEN requested_preview AND preview_status = 'pending' THEN $2 ELSE preview_failure_detail END,
			analysis_status = CASE WHEN requested_analysis AND analysis_status = 'pending' THEN 'failed' ELSE analysis_status END,
			analysis_failure_code = CASE WHEN requested_analysis AND analysis_status = 'pending' THEN $1 ELSE analysis_failure_code END,
			analysis_failure_detail = CASE WHEN requested_analysis AND analysis_status = 'pending' THEN $2 ELSE analysis_failure_detail END
		WHERE state = 'running' AND lease_expires_at <= $3
		RETURNING id`,
		jobs.InterruptedCode, jobs.InterruptedDetail, now)
	if err != nil {
		return nil, fmt.Errorf("fail interrupted jobs: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan interrupted job: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interrupted jobs: %w", err)
	}
	return ids, nil
}
