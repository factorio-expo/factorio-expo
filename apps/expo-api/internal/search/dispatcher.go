package search

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/outbox"
)

const (
	actionIndex  = "index"
	actionDelete = "delete"
	// maxRetries bounds automatic retries for a failing sync before the job
	// is marked failed and left for a manual/full reindex to pick up.
	maxRetries = 5
	// leaseDuration must exceed one Typesense round trip with its timeout.
	leaseDuration = time.Minute
)

// Dispatcher reconciles the entry_search_jobs durable queue against a Client.
// The poll loop, lease, retry budget and backoff are internal/outbox's; this
// type is its search store and handler.
type Dispatcher struct {
	queue *outbox.Queue[db.EntrySearchJob]
}

// Options configures a Dispatcher.
type Options struct {
	PollInterval time.Duration
	BatchSize    int32
}

// New builds a Dispatcher bound to pool and client.
func New(pool *pgxpool.Pool, client Indexer, options Options) *Dispatcher {
	return newDispatcher(db.New(pool), client, options)
}

func newDispatcher(queries searchQueries, client Indexer, options Options) *Dispatcher {
	if options.PollInterval <= 0 {
		options.PollInterval = 5 * time.Second
	}
	return &Dispatcher{queue: outbox.New[db.EntrySearchJob](
		"search", &jobStore{queries: queries}, &handler{client: client},
		outbox.Policy{
			MaxRetries: maxRetries, Base: 5 * time.Second, Max: 5 * time.Minute, Lease: leaseDuration,
		},
		outbox.Options{PollInterval: options.PollInterval, BatchSize: options.BatchSize},
	)}
}

// Run polls entry_search_jobs until ctx is canceled.
func (d *Dispatcher) Run(ctx context.Context) { d.queue.Run(ctx) }

// PollOnce reconciles every due entry_search_jobs row against the client.
func (d *Dispatcher) PollOnce(ctx context.Context) error { return d.queue.PollOnce(ctx) }

// Nudge asks the loop to poll now.
func (d *Dispatcher) Nudge() { d.queue.Nudge() }

// handler syncs one entry: a Typesense write is always resolved in one pass.
type handler struct{ client Indexer }

func (h *handler) Handle(ctx context.Context, job db.EntrySearchJob) []outbox.Result {
	entryID := job.EntryID
	var err error
	switch job.Action {
	case actionDelete:
		err = h.client.DeleteEntry(ctx, entryID)
	default:
		err = h.client.IndexEntry(ctx, entryID)
	}
	if err != nil {
		return []outbox.Result{{Outcome: outbox.Transient("sync_failed", err.Error())}}
	}
	return []outbox.Result{{Outcome: outbox.Succeeded()}}
}

// searchQueries is the slice of the generated queries the store uses.
type searchQueries interface {
	ClaimSearchJobs(ctx context.Context, arg db.ClaimSearchJobsParams) ([]db.EntrySearchJob, error)
	DeleteSearchJob(ctx context.Context, arg db.DeleteSearchJobParams) error
	RetrySearchJob(ctx context.Context, arg db.RetrySearchJobParams) error
	ReleaseSearchJob(ctx context.Context, arg db.ReleaseSearchJobParams) error
	MarkSearchJobFailed(ctx context.Context, arg db.MarkSearchJobFailedParams) error
}

type jobStore struct{ queries searchQueries }

func (s *jobStore) Claim(ctx context.Context, limit int32, lease time.Duration) ([]db.EntrySearchJob, error) {
	jobs, err := s.queries.ClaimSearchJobs(ctx, db.ClaimSearchJobsParams{
		LeaseSeconds: lease.Seconds(), BatchSize: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("claim search jobs: %w", err)
	}
	return jobs, nil
}

func (s *jobStore) Retries(job db.EntrySearchJob, _ string) int32 { return job.RetryCount }

// Apply resolves the job as claimed: every query is guarded by the claimed
// row's updated_at, so an entry re-enqueued during the sync keeps its newer
// intent (a delete queued while an index ran is not deleted with the job).
func (s *jobStore) Apply(
	ctx context.Context, job db.EntrySearchJob, _ string, resolution outbox.Resolution,
) error {
	at := pgtype.Timestamptz{Time: resolution.At, Valid: true}
	switch resolution.Kind {
	case outbox.ResolveDone:
		return s.queries.DeleteSearchJob(ctx, db.DeleteSearchJobParams{
			EntryID: job.EntryID, ClaimedUpdatedAt: job.UpdatedAt,
		})
	case outbox.ResolveRetry:
		return s.queries.RetrySearchJob(ctx, db.RetrySearchJobParams{
			EntryID: job.EntryID, ClaimedUpdatedAt: job.UpdatedAt,
			FailureCode: resolution.Code, FailureDetail: resolution.Detail, AvailableAt: at,
		})
	case outbox.ResolveFail:
		return s.queries.MarkSearchJobFailed(ctx, db.MarkSearchJobFailedParams{
			EntryID: job.EntryID, ClaimedUpdatedAt: job.UpdatedAt,
			FailureCode: resolution.Code, FailureDetail: resolution.Detail,
		})
	case outbox.ResolveRelease:
		return s.queries.ReleaseSearchJob(ctx, db.ReleaseSearchJobParams{
			EntryID: job.EntryID, ClaimedUpdatedAt: job.UpdatedAt, AvailableAt: at,
		})
	}
	return fmt.Errorf("cannot apply resolution %d", resolution.Kind)
}
