package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func (s *Store) Get(ctx context.Context, id string) (jobs.Job, error) {
	var job jobs.Job
	var state, previewStatus, analysisStatus string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, blueprint, state, failure_code, failure_detail,
		       requested_preview, preview_status, preview_failure_code, preview_failure_detail,
		       requested_analysis, analysis_status, analysis_failure_code, analysis_failure_detail,
		       created_at, updated_at
		FROM runner_jobs WHERE id = $1`, id).
		Scan(&job.ID, &job.Blueprint, &state, &job.FailureCode, &job.FailureDetail,
			&job.PreviewPhase.Requested, &previewStatus, &job.PreviewPhase.FailureCode, &job.PreviewPhase.FailureDetail,
			&job.AnalysisPhase.Requested, &analysisStatus, &job.AnalysisPhase.FailureCode, &job.AnalysisPhase.FailureDetail,
			&job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, ErrNotFound
	}
	if err != nil {
		return jobs.Job{}, fmt.Errorf("get job: %w", err)
	}
	job.State = jobs.State(state)
	job.PreviewPhase.Status = jobs.PhaseStatus(previewStatus)
	job.AnalysisPhase.Status = jobs.PhaseStatus(analysisStatus)
	if err := s.loadArtifacts(ctx, &job); err != nil {
		return jobs.Job{}, err
	}
	return job, nil
}

func (s *Store) loadArtifacts(ctx context.Context, job *jobs.Job) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, phase, relative_path, state, media_type, size, sha256,
		       width, height, created_at, expires_at, deleted_at
		FROM runner_artifacts WHERE job_id = $1`, job.ID)
	if err != nil {
		return fmt.Errorf("load job artifacts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var artifact jobs.Artifact
		var state string
		var deleted sql.NullTime
		if err := rows.Scan(
			&artifact.JobID, &artifact.Phase, &artifact.RelativePath, &state,
			&artifact.MediaType, &artifact.Size, &artifact.SHA256, &artifact.Width,
			&artifact.Height, &artifact.CreatedAt, &artifact.ExpiresAt, &deleted,
		); err != nil {
			return fmt.Errorf("scan job artifact: %w", err)
		}
		artifact.State = jobs.ArtifactState(state)
		if deleted.Valid {
			value := deleted.Time
			artifact.DeletedAt = &value
		}
		switch artifact.Phase {
		case jobs.PhasePreview:
			job.PreviewArtifact = &artifact
		case jobs.PhaseAnalysis:
			job.AnalysisArtifact = &artifact
		default:
			return fmt.Errorf("load job artifacts: unknown phase %q", artifact.Phase)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate job artifacts: %w", err)
	}
	return nil
}

func (s *Store) CompletePhase(ctx context.Context, workerID, id string, phase jobs.Phase, artifact *jobs.Artifact) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin complete job: %w", err)
	}

	defer tx.Rollback()
	statusColumn, failureCodeColumn, failureDetailColumn := phaseColumns(phase)
	result, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE runner_jobs SET %s = 'succeeded', %s = '', %s = '', updated_at = $1
		WHERE id = $2 AND state = 'running' AND claimed_by = $3 AND %s = 'pending'`,
		statusColumn, failureCodeColumn, failureDetailColumn, statusColumn), now, id, workerID)
	if err != nil {
		return fmt.Errorf("complete phase: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("complete phase: claim no longer owned")
	}
	if artifact != nil {
		artifact.Phase = phase
		if _, err := tx.ExecContext(ctx, `
		INSERT INTO runner_artifacts(job_id, phase, relative_path, state, media_type, size, sha256,
		width, height, created_at, expires_at)
		VALUES ($1, $2, $3, 'available', $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT(job_id, phase) DO UPDATE SET
			relative_path=excluded.relative_path, state='available', media_type=excluded.media_type,
			size=excluded.size, sha256=excluded.sha256, width=excluded.width, height=excluded.height,
			created_at=excluded.created_at, expires_at=excluded.expires_at, deleted_at=NULL`,
			artifact.JobID, artifact.Phase, artifact.RelativePath, artifact.MediaType, artifact.Size,
			artifact.SHA256, artifact.Width, artifact.Height, artifact.CreatedAt,
			artifact.ExpiresAt); err != nil {
			return fmt.Errorf("save artifact: %w", err)
		}
	}
	if err := finalizeClaimTx(ctx, tx, id, workerID, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit completed job: %w", err)
	}
	return nil
}

func (s *Store) Complete(ctx context.Context, workerID string, artifact jobs.Artifact) error {
	if err := s.CompletePhase(ctx, workerID, artifact.JobID, jobs.PhasePreview, &artifact); err != nil {
		return err
	}
	return s.CompletePhase(ctx, workerID, artifact.JobID, jobs.PhaseAnalysis, nil)
}

func (s *Store) FailPhase(ctx context.Context, workerID, id string, phase jobs.Phase, code, detail string) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin phase failure: %w", err)
	}
	defer tx.Rollback()
	statusColumn, failureCodeColumn, failureDetailColumn := phaseColumns(phase)
	result, err := tx.ExecContext(ctx, fmt.Sprintf(`
			UPDATE runner_jobs SET %s = 'failed', %s = $1, %s = $2,
				failure_code = $1, failure_detail = $2, updated_at = $3
			WHERE id = $4 AND state = 'running' AND claimed_by = $5 AND %s = 'pending'`,
		statusColumn, failureCodeColumn, failureDetailColumn, statusColumn), code, detail, now, id, workerID)
	if err != nil {
		return fmt.Errorf("fail phase: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("fail phase: claim no longer owned")
	}
	if err := finalizeClaimTx(ctx, tx, id, workerID, now); err != nil {
		return err
	}
	return tx.Commit()
}

// ExpireLeases ends the lease of every running job. Open calls it: a job
// still running when the runner starts was interrupted, and the first
// ClaimNext fails it as retryable instead of waiting out its lease.
func (s *Store) ExpireLeases(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE runner_jobs SET lease_expires_at = $1
		WHERE state = 'running'`, time.Now().UTC()); err != nil {
		return fmt.Errorf("expire job leases: %w", err)
	}
	return nil
}

func (s *Store) CleanupSucceededJobs(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM runner_jobs
		WHERE state IN ('succeeded', 'failed')
		  AND NOT EXISTS (
		      SELECT 1 FROM runner_artifacts
		      WHERE runner_artifacts.job_id = runner_jobs.id
		        AND runner_artifacts.state = 'available'
		  )`)
	if err != nil {
		return 0, fmt.Errorf("cleanup succeeded jobs: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count cleaned up succeeded jobs: %w", err)
	}
	return affected, nil
}

func phaseColumns(phase jobs.Phase) (status, code, detail string) {
	switch phase {
	case jobs.PhasePreview:
		return "preview_status", "preview_failure_code", "preview_failure_detail"
	case jobs.PhaseAnalysis:
		return "analysis_status", "analysis_failure_code", "analysis_failure_detail"
	default:
		panic("unknown phase")
	}
}

func finalizeClaimTx(ctx context.Context, tx *sql.Tx, id, workerID string, now time.Time) error {
	var previewRequested, analysisRequested bool
	var previewStatus, analysisStatus string
	if err := tx.QueryRowContext(ctx, `
			SELECT requested_preview, preview_status, requested_analysis, analysis_status
			FROM runner_jobs WHERE id = $1 AND state = 'running' AND claimed_by = $2`,
		id, workerID).Scan(&previewRequested, &previewStatus, &analysisRequested, &analysisStatus); err != nil {
		return fmt.Errorf("read phase statuses: %w", err)
	}
	terminal := (!previewRequested || previewStatus != string(jobs.PhasePending)) &&
		(!analysisRequested || analysisStatus != string(jobs.PhasePending))
	if !terminal {
		return nil
	}
	state := jobs.StateSucceeded
	if (previewRequested && previewStatus == string(jobs.PhaseFailed)) ||
		(analysisRequested && analysisStatus == string(jobs.PhaseFailed)) {
		state = jobs.StateFailed
	}
	result, err := tx.ExecContext(ctx, `
			UPDATE runner_jobs SET state = $1, claimed_by = NULL, lease_expires_at = NULL, updated_at = $2
			WHERE id = $3 AND state = 'running' AND claimed_by = $4`, state, now, id, workerID)
	if err != nil {
		return fmt.Errorf("finalize job: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("finalize job: claim no longer owned")
	}
	return nil
}
