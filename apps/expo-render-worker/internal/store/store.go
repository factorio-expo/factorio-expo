package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/jobs"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	s := &Store{db: db}
	if err := s.checkSchema(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.ExpireLeases(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ready(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// checkSchema fails fast when the runner database has not been migrated
// (apps/expo-render-worker/migrations, applied by expo-db-setup).
func (s *Store) checkSchema(ctx context.Context) error {
	var jobs, artifacts sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT to_regclass('runner_jobs')::text, to_regclass('runner_artifacts')::text`,
	).Scan(&jobs, &artifacts)
	if err != nil {
		return fmt.Errorf("check runner schema: %w", err)
	}
	if !jobs.Valid || !artifacts.Valid {
		return errors.New("runner database is not migrated: apply apps/expo-render-worker/migrations")
	}
	return nil
}

func (s *Store) Create(ctx context.Context, blueprint string, requested ...[]jobs.Phase) (jobs.Job, error) {
	return s.CreateIdempotent(ctx, "", blueprint, requested...)
}

// CreateIdempotent queues a job under a client-supplied key. A repeat of the
// same non-empty key returns the existing job instead of queueing another.
func (s *Store) CreateIdempotent(ctx context.Context, key, blueprint string, requested ...[]jobs.Phase) (jobs.Job, error) {
	id, err := newID()
	if err != nil {
		return jobs.Job{}, err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("begin create job: %w", err)
	}
	defer tx.Rollback()
	phases := []jobs.Phase{jobs.PhasePreview, jobs.PhaseAnalysis}
	if len(requested) > 0 {
		phases = requested[0]
	}
	requestedPreview, requestedAnalysis := false, false
	for _, phase := range phases {
		switch phase {
		case jobs.PhasePreview:
			requestedPreview = true
		case jobs.PhaseAnalysis:
			requestedAnalysis = true
		}
	}
	previewStatus, analysisStatus := jobs.PhaseNotRequested, jobs.PhaseNotRequested
	if requestedPreview {
		previewStatus = jobs.PhasePending
	}
	if requestedAnalysis {
		analysisStatus = jobs.PhasePending
	}
	var keyValue sql.NullString
	if key != "" {
		keyValue = sql.NullString{String: key, Valid: true}
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO runner_jobs(id, blueprint, state, requested_preview, requested_analysis,
			preview_status, analysis_status, created_at, updated_at, available_at, idempotency_key)
		VALUES ($1, $2, 'queued', $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (idempotency_key) DO NOTHING`,
		id, blueprint, requestedPreview, requestedAnalysis, previewStatus, analysisStatus, now, now, now, keyValue)
	if err != nil {
		return jobs.Job{}, fmt.Errorf("insert job: %w", err)
	}
	if inserted, _ := result.RowsAffected(); inserted == 0 {
		if err := s.db.QueryRowContext(ctx,
			`SELECT id FROM runner_jobs WHERE idempotency_key = $1`, key).Scan(&id); err != nil {
			return jobs.Job{}, fmt.Errorf("load idempotent job: %w", err)
		}
		return s.Get(ctx, id)
	}
	if err := tx.Commit(); err != nil {
		return jobs.Job{}, fmt.Errorf("commit job: %w", err)
	}
	return s.Get(ctx, id)
}

func newID() (string, error) {
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate job id: %w", err)
	}
	return "pj_" + base64.RawURLEncoding.EncodeToString(random), nil
}
