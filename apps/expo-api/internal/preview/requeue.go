package preview

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

// requeueQueries is Requeuer's slice of the job table; *db.Queries
// satisfies it.
type requeueQueries interface {
	RequeuePhaseJob(ctx context.Context, arg db.RequeuePhaseJobParams) (string, error)
	CountWaitingLeaves(ctx context.Context, queueKey string) (int64, error)
	LockQueueKey(ctx context.Context, queueKey string) error
	LastOrderAt(ctx context.Context, queueKey string) (pgtype.Timestamptz, error)
}

// leafProgress reads a leaf's phase states as stored (Catalog.LeafProgress).
type leafProgress interface {
	LeafProgress(ctx context.Context, blueprintID string) (leaf progress.Leaf, found bool, err error)
}

// Requeuer runs a leaf's phase again: the one way work is queued after the
// upload (a re-render of an evicted full preview now; re-running failed jobs
// and re-analysis later). It writes the intent, tells open event streams the
// phase is pending and wakes the dispatcher, so clients follow it exactly like
// an upload. A stored result stays shown while it runs and when it fails
// (progress.LeafOf).
type Requeuer struct {
	queries  requeueQueries
	catalog  leafProgress
	progress *progress.Hub
	// queued wakes the dispatcher after a new intent.
	queued func()
}

func NewRequeuer(queries requeueQueries, catalog leafProgress, hub *progress.Hub, queued func()) *Requeuer {
	return &Requeuer{queries: queries, catalog: catalog, progress: hub, queued: queued}
}

// Requeue queues blueprintID's phase (progress.PhasePreview or
// PhaseAnalysis) under ticket: the requester's place in the render queue,
// refused with renderqueue.ErrFull when the requester already has their cap
// of waiting leaves. An open intent makes it a no-op; a failed one is reset
// only with retryFailed. queued reports whether anything was queued.
func (r *Requeuer) Requeue(
	ctx context.Context, blueprintID, phase string, retryFailed bool, ticket renderqueue.Ticket,
) (queued bool, err error) {
	if err := renderqueue.Check(ctx, r.queries, ticket, 1); err != nil {
		return false, err
	}
	places, err := renderqueue.Place(ctx, r.queries, ticket, time.Now(), 1)
	if err != nil {
		return false, err
	}
	if phase != progress.PhasePreview && phase != progress.PhaseAnalysis {
		return false, fmt.Errorf("requeue: unknown phase %q", phase)
	}
	entryID, err := r.queries.RequeuePhaseJob(ctx, db.RequeuePhaseJobParams{
		BlueprintID: blueprintID, Phase: phase, RetryFailed: retryFailed, QueueKey: ticket.Key, OrderAt: places[0],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("requeue %s of %s: %w", phase, blueprintID, err)
	}
	// Streams get the phase as stored now, like the dispatcher's events.
	leaf, found, err := r.catalog.LeafProgress(ctx, blueprintID)
	if err != nil {
		return true, err
	}
	if found {
		r.progress.Publish(progress.Change{
			EntryID: entryID, BlueprintID: blueprintID, Phase: phase, State: leaf.State(phase),
		})
	}
	if r.queued != nil {
		r.queued()
	}
	return true, nil
}
