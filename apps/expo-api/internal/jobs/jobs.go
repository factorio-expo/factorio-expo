// Package jobs is the staff view of background jobs that failed for good
// (preview, analysis and search sync) and their re-runs (docs/admin.md).
package jobs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

// The job kinds: a leaf's two phases and the entry's search sync.
const (
	KindPreview  = progress.PhasePreview
	KindAnalysis = progress.PhaseAnalysis
	KindSearch   = "search"
)

// listLimit bounds the failed jobs one page shows.
const listLimit = 500

var (
	// ErrUnknownKind is a kind that is not a job kind.
	ErrUnknownKind = fault.New(fault.Invalid, "unknown_job_kind", "unknown job kind")
	// ErrNotFailed is a job that is not (or no longer) failed.
	ErrNotFailed = fault.New(fault.NotFound, "job_not_failed", "the job is not failed")
	// ErrAlreadyQueued is a re-run of leaves whose phase is queued already.
	ErrAlreadyQueued = fault.New(fault.Conflict, "already_queued", "already queued")
)

// Failed is one job that failed for good, with why.
type Failed struct {
	Kind string `json:"kind"`
	// TargetID is the blueprint (preview, analysis) or entry (search).
	TargetID       string `json:"target_id"`
	EntryID        string `json:"entry_id"`
	EntryTitle     string `json:"entry_title"`
	EntryLive      bool   `json:"entry_live"`
	BlueprintLabel string `json:"blueprint_label"`
	// SearchAction is "index" or "delete" for a search job, else empty.
	SearchAction  string    `json:"search_action"`
	FailureCode   string    `json:"failure_code"`
	FailureDetail string    `json:"failure_detail"`
	RetryCount    int32     `json:"retry_count"`
	FailedAt      time.Time `json:"failed_at"`
}

type queries interface {
	ListFailedJobs(ctx context.Context, arg db.ListFailedJobsParams) ([]db.ListFailedJobsRow, error)
	RetryFailedSearchJob(ctx context.Context, entryID string) (int64, error)
	ListLiveEntryLeafIDs(ctx context.Context, entryID string) ([]string, error)
}

// requeuer runs a leaf's phase again (preview.Requeuer).
type requeuer interface {
	Requeue(ctx context.Context, blueprintID, phase string, retryFailed bool, ticket renderqueue.Ticket) (bool, error)
}

type auditor interface {
	Add(ctx context.Context, by audit.Staff, record audit.Record) error
}

// Service lists failed jobs and re-runs them.
type Service struct {
	queries  queries
	requeuer requeuer
	audit    auditor
	// searchQueued wakes the search dispatcher after a re-run.
	searchQueued func()
}

func New(queries queries, requeuer requeuer, audit auditor, searchQueued func()) *Service {
	return &Service{queries: queries, requeuer: requeuer, audit: audit, searchQueued: searchQueued}
}

// ListFailed lists failed jobs newest first; kind "" means every kind.
func (s *Service) ListFailed(ctx context.Context, kind string) ([]Failed, error) {
	params := db.ListFailedJobsParams{RowLimit: listLimit}
	if kind != "" {
		if !validKind(kind) {
			return nil, ErrUnknownKind
		}
		params.Kind = pgtype.Text{String: kind, Valid: true}
	}
	return s.list(ctx, params)
}

// Rerun queues a failed job again with a fresh retry budget. The audit log
// gets the failure it replaces before the job is queued, so a re-run is never
// unaudited (should the queueing then fail, the row only says it was asked).
func (s *Service) Rerun(ctx context.Context, by audit.Staff, kind, targetID, reason string) error {
	if !validKind(kind) {
		return ErrUnknownKind
	}
	// Checked before queueing, so the audit row cannot be refused after.
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	failed, err := s.list(ctx, db.ListFailedJobsParams{
		Kind: pgtype.Text{String: kind, Valid: true}, TargetID: pgtype.Text{String: targetID, Valid: true}, RowLimit: 1,
	})
	if err != nil {
		return err
	}
	if len(failed) == 0 {
		return ErrNotFailed
	}
	job := failed[0]
	if err := s.audit.Add(ctx, by, audit.Record{
		Action: "job.rerun", TargetType: kind + "_job", TargetID: targetID, Reason: reason,
		Before: map[string]any{
			"entry_id": job.EntryID, "failure_code": job.FailureCode,
			"failure_detail": job.FailureDetail, "retry_count": job.RetryCount,
		},
		After: map[string]any{"state": "pending"},
	}); err != nil {
		return err
	}
	queued := false
	if kind == KindSearch {
		rows, err := s.queries.RetryFailedSearchJob(ctx, targetID)
		if err != nil {
			return fmt.Errorf("re-run search job of %s: %w", targetID, err)
		}
		queued = rows > 0
		if queued && s.searchQueued != nil {
			s.searchQueued()
		}
	} else if queued, err = s.requeuer.Requeue(ctx, targetID, kind, true, renderqueue.Staff()); err != nil {
		return err
	}
	if !queued {
		return ErrNotFailed
	}
	return nil
}

// RerunEntry queues a phase (preview or analysis) of a live entry's current
// revision again (audited first, like Rerun), done or failed: one blueprint, or every one when
// blueprintID is empty. A leaf whose phase is queued already is skipped;
// ErrAlreadyQueued when nothing was queued. A stored result stays shown
// until the new one replaces it.
func (s *Service) RerunEntry(ctx context.Context, by audit.Staff, entryID, phase, blueprintID, reason string) error {
	if phase != KindPreview && phase != KindAnalysis {
		return ErrUnknownKind
	}
	reason, err := audit.CleanReason(reason)
	if err != nil {
		return err
	}
	leaves, err := s.queries.ListLiveEntryLeafIDs(ctx, entryID)
	if err != nil {
		return fmt.Errorf("list leaves of %s: %w", entryID, err)
	}
	if blueprintID != "" {
		if !slices.Contains(leaves, blueprintID) {
			return catalog.ErrNotFound
		}
		leaves = []string{blueprintID}
	}
	if len(leaves) == 0 {
		return catalog.ErrNotFound
	}
	after := map[string]any{"phase": phase}
	if blueprintID != "" {
		after["blueprint_id"] = blueprintID
	}
	if err := s.audit.Add(ctx, by, audit.Record{
		Action: "entry.rerun", TargetType: "entry", TargetID: entryID, Reason: reason, After: after,
	}); err != nil {
		return err
	}
	queued := 0
	for _, leaf := range leaves {
		ok, err := s.requeuer.Requeue(ctx, leaf, phase, true, renderqueue.Staff())
		if err != nil {
			return err
		}
		if ok {
			queued++
		}
	}
	if queued == 0 {
		return ErrAlreadyQueued
	}
	return nil
}

func (s *Service) list(ctx context.Context, params db.ListFailedJobsParams) ([]Failed, error) {
	rows, err := s.queries.ListFailedJobs(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list failed jobs: %w", err)
	}
	failed := make([]Failed, len(rows))
	for i, row := range rows {
		failed[i] = Failed{
			Kind: row.Kind, TargetID: row.TargetID, EntryID: row.EntryID, EntryTitle: row.EntryTitle,
			EntryLive: row.EntryLive, BlueprintLabel: row.BlueprintLabel, SearchAction: row.SearchAction,
			FailureCode: row.FailureCode, FailureDetail: row.FailureDetail, RetryCount: row.RetryCount,
			FailedAt: row.FailedAt.Time,
		}
	}
	return failed, nil
}

func validKind(kind string) bool {
	return kind == KindPreview || kind == KindAnalysis || kind == KindSearch
}
