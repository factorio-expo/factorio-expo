package preview

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/outbox"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/internal/runnerapi"
)

const (
	slotPreview  = string(runnerapi.PhasePreview)
	slotAnalysis = string(runnerapi.PhaseAnalysis)
)

// storeQueries is the slice of the generated queries the job store uses.
type storeQueries interface {
	ClaimDispatchJobs(ctx context.Context, arg db.ClaimDispatchJobsParams) ([]db.ClaimDispatchJobsRow, error)
	DeletePhaseJob(ctx context.Context, arg db.DeletePhaseJobParams) error
	RetryPhaseJob(ctx context.Context, arg db.RetryPhaseJobParams) error
	ReleasePhaseJob(ctx context.Context, arg db.ReleasePhaseJobParams) error
	MarkPhaseJobFailed(ctx context.Context, arg db.MarkPhaseJobFailedParams) error
}

// jobStore adapts phase_jobs: one claimed item is one
// blueprint with a "preview" and an "analysis" slot. Once a slot's final
// state is persisted it is published to the entry's progress streams.
type jobStore struct {
	queries  storeQueries
	catalog  Catalog
	progress *progress.Hub
	// queue sets the dispatch window and the spacing of one key's leaves.
	queue renderqueue.Policy
}

// Claim first gives waiting leaves results that identical leaves stored
// meanwhile (they never reach the runner), then claims in-flight rows to poll
// and the best waiting leaves that fit the dispatch window
// (ClaimDispatchJobs).
func (s *jobStore) Claim(ctx context.Context, limit int32, lease time.Duration) ([]db.ClaimDispatchJobsRow, error) {
	if err := s.reuse(ctx); err != nil {
		return nil, err
	}
	rows, err := s.queries.ClaimDispatchJobs(ctx, db.ClaimDispatchJobsParams{
		LeaseSeconds: lease.Seconds(), BatchSize: limit,
		DispatchWindow: int64(s.queue.Window),
	})
	if err != nil {
		return nil, fmt.Errorf("claim blueprint jobs: %w", err)
	}
	return rows, nil
}

// reuse settles waiting leaves from identical leaves' stored results and
// tells their streams, like a finished job: a copied final failure of a quick
// preview deletes it.
func (s *jobStore) reuse(ctx context.Context) error {
	reused, err := s.catalog.ReuseWaitingResults(ctx)
	if err != nil {
		return fmt.Errorf("reuse waiting results: %w", err)
	}
	rejected := make(map[string]bool)
	for _, leaf := range reused {
		state, found, err := s.publishLeaf(ctx, leaf.EntryID, leaf.BlueprintID, leaf.Phase)
		if err != nil {
			return err
		}
		if found && leaf.FailureCode != "" && state.Status == progress.StatusFailed && !rejected[leaf.EntryID] {
			rejected[leaf.EntryID] = true
			if err := s.catalog.RejectQuickPreview(ctx, leaf.EntryID); err != nil {
				return err
			}
		}
	}
	return nil
}

// publishLeaf tells open streams a leaf phase as stored now.
func (s *jobStore) publishLeaf(
	ctx context.Context, entryID, blueprintID, phase string,
) (state progress.PhaseState, found bool, err error) {
	leaf, found, err := s.catalog.LeafProgress(ctx, blueprintID)
	if err != nil || !found {
		return state, found, err
	}
	state = leaf.State(phase)
	s.progress.Publish(progress.Change{EntryID: entryID, BlueprintID: blueprintID, Phase: phase, State: state})
	return state, true, nil
}

func (s *jobStore) Retries(item db.ClaimDispatchJobsRow, slot string) int32 {
	if slot == slotAnalysis {
		return item.AnalysisRetryCount.Int32
	}
	return item.PreviewRetryCount.Int32
}

func (s *jobStore) Apply(
	ctx context.Context, item db.ClaimDispatchJobsRow, slot string, resolution outbox.Resolution,
) error {
	if err := s.apply(ctx, item, slot, resolution); err != nil {
		return err
	}
	if resolution.Kind != outbox.ResolveDone && resolution.Kind != outbox.ResolveFail {
		// Retries and releases leave the phase pending: nothing to tell.
		return nil
	}
	// Streams get the phase as stored now (the same rule as snapshots), not
	// as this resolution would suggest: a leaf that already has a result
	// stays ready when a later job fails.
	state, found, err := s.publishLeaf(ctx, item.EntryID, item.BlueprintID, slot)
	if err != nil || !found {
		return err
	}
	if resolution.Kind == outbox.ResolveFail && state.Status == progress.StatusFailed {
		// A quick preview whose phase failed for good is deleted (other
		// entries are left alone). The failed phase was published first, so
		// open streams report its code before they end with `gone`. A failed
		// re-render of an evicted full file leaves the stored preview ready.
		return s.catalog.RejectQuickPreview(ctx, item.EntryID)
	}
	return nil
}

func (s *jobStore) apply(
	ctx context.Context, item db.ClaimDispatchJobsRow, slot string, resolution outbox.Resolution,
) error {
	at := pgtype.Timestamptz{Time: resolution.At, Valid: true}
	switch resolution.Kind {
	case outbox.ResolveDone:
		return s.queries.DeletePhaseJob(ctx, db.DeletePhaseJobParams{BlueprintID: item.BlueprintID, Phase: slot})
	case outbox.ResolveRetry:
		return s.queries.RetryPhaseJob(ctx, db.RetryPhaseJobParams{
			BlueprintID: item.BlueprintID, Phase: slot, FailureCode: resolution.Code,
			FailureDetail: resolution.Detail, AvailableAt: at,
		})
	case outbox.ResolveFail:
		if slot == slotAnalysis {
			// The job is marked failed in the same transaction that
			// stales the stored production.
			return s.catalog.AnalysisFailed(ctx, item.BlueprintID, resolution.Code, resolution.Detail)
		}
		return s.queries.MarkPhaseJobFailed(ctx, db.MarkPhaseJobFailedParams{
			BlueprintID: item.BlueprintID, Phase: slot, FailureCode: resolution.Code, FailureDetail: resolution.Detail,
		})
	case outbox.ResolveRelease:
		return s.queries.ReleasePhaseJob(ctx, db.ReleasePhaseJobParams{
			BlueprintID: item.BlueprintID, Phase: slot, AvailableAt: at,
		})
	}
	return fmt.Errorf("cannot apply resolution %d to slot %q", resolution.Kind, slot)
}
