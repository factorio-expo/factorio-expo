package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func (s *Store) GetArtifact(ctx context.Context, jobID string, phase jobs.Phase) (jobs.Artifact, error) {
	return s.getArtifact(ctx, jobID, phase)
}

func (s *Store) getArtifact(ctx context.Context, jobID string, phase jobs.Phase) (jobs.Artifact, error) {
	var artifact jobs.Artifact
	var state string
	var deleted sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT job_id, phase, relative_path, state, media_type, size, sha256,
		       width, height, created_at, expires_at, deleted_at
		FROM runner_artifacts WHERE job_id = $1 AND phase = $2`, jobID, phase).
		Scan(&artifact.JobID, &artifact.Phase, &artifact.RelativePath, &state, &artifact.MediaType,
			&artifact.Size, &artifact.SHA256, &artifact.Width, &artifact.Height,
			&artifact.CreatedAt, &artifact.ExpiresAt, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.Artifact{}, ErrNotFound
	}
	if err != nil {
		return jobs.Artifact{}, fmt.Errorf("get artifact: %w", err)
	}
	artifact.State = jobs.ArtifactState(state)
	if deleted.Valid {
		value := deleted.Time
		artifact.DeletedAt = &value
	}
	return artifact, nil
}

func (s *Store) MarkArtifactDeleted(ctx context.Context, jobID string, phase jobs.Phase, state jobs.ArtifactState) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE runner_artifacts SET state = $1, deleted_at = $2
		WHERE job_id = $3 AND phase = $4`, string(state), time.Now().UTC(), jobID, phase)
	if err != nil {
		return fmt.Errorf("mark artifact deleted: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ExpiredArtifacts(ctx context.Context, now time.Time) ([]jobs.Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id, phase FROM runner_artifacts
		WHERE state = 'available' AND expires_at <= $1`, now)
	if err != nil {
		return nil, fmt.Errorf("list expired artifacts: %w", err)
	}
	var keys []struct {
		jobID string
		phase jobs.Phase
	}
	for rows.Next() {
		var key struct {
			jobID string
			phase jobs.Phase
		}
		if err := rows.Scan(&key.jobID, &key.phase); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan expired artifact: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close expired artifacts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]jobs.Artifact, 0, len(keys))
	for _, key := range keys {
		artifact, err := s.getArtifact(ctx, key.jobID, key.phase)
		if err != nil {
			return nil, err
		}
		result = append(result, artifact)
	}
	return result, nil
}
