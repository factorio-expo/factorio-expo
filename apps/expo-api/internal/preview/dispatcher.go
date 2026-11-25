// Package preview dispatches rendering and production-analysis work for
// catalog blueprints to the preview runner and imports finished results. The
// poll loop, leases, retry budget and backoff come from internal/outbox; this
// package supplies the table store and the handler. The runner runs each job
// once and flags whether a failure is retryable; retries are the API's alone.
// It owns runner I/O, encoding, the preview files and its job tables; every
// entry, preview and production row it changes goes through Catalog.
package preview

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/outbox"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/internal/analysisresult"
)

const (
	// maxRetries is the per-phase retry budget before a phase fails.
	maxRetries = 3
	// leaseDuration must exceed the slowest handler (runner polling plus an
	// analysis download and import).
	leaseDuration = 2 * time.Minute
)

// Catalog is the catalog side of the dispatcher: it changes the entry,
// preview and production rows for a result or a final failure, with the
// search intent in the same transaction and open streams told after it.
// *catalog.Service implements it.
type Catalog interface {
	// StorePreview references stored files from the blueprint's previews row,
	// or queues them for the file sweep when no row takes them.
	StorePreview(ctx context.Context, blueprintID, sourceSHA256 string, width, height int) error
	StoreProduction(ctx context.Context, blueprintID string, production analysisresult.Stored) error
	// AnalysisFailed marks the leaf's analysis job failed and its stored
	// production stale, in one transaction.
	AnalysisFailed(ctx context.Context, blueprintID, code, detail string) error
	// RejectQuickPreview deletes a quick preview whose phase failed for good.
	RejectQuickPreview(ctx context.Context, entryID string) error
	// LeafProgress is the leaf's phase states as stored (progress.LeafOf).
	// found is false once the leaf is gone.
	LeafProgress(ctx context.Context, blueprintID string) (leaf progress.Leaf, found bool, err error)
	// ReuseWaitingResults gives waiting leaves the result another leaf with
	// the same string has stored (catalog reuse) and returns them.
	ReuseWaitingResults(ctx context.Context) ([]renderqueue.ReusedLeaf, error)
}

// Dispatcher durably records phase work, submits it to the runner, and polls
// in-flight jobs until previews and production snapshots are imported.
type Dispatcher struct {
	reconcile func(context.Context) error
	queue     *outbox.Queue[db.ClaimDispatchJobsRow]
	imports   *importer
}

type Options struct {
	PollInterval time.Duration
	BatchSize    int32
	// Encoders caps parallel preview encodes; 0 means defaultEncoders.
	Encoders int
	// Progress receives every phase that became ready or failed; nil drops them.
	Progress *progress.Hub
	// Budget bounds the preview files' disk use, checked every minute.
	Budget Budget
	// Queue orders what is sent to the runner (window, leaf gap) and places
	// repaired intents; the zero value means renderqueue.Defaults().
	Queue renderqueue.Policy
}

func New(pool *pgxpool.Pool, runnerClient *runner.Client, files *Files, catalog Catalog, options Options) *Dispatcher {
	queries := db.New(pool)
	dispatcher := newDispatcher(queries, queries, catalog, runnerClient, files, options)
	dispatcher.reconcile = func(ctx context.Context) error {
		if err := files.sweep(ctx, queries); err != nil {
			return err
		}
		if err := files.enforceBudget(ctx, queries, options.Budget); err != nil {
			return err
		}
		return queries.ReconcileMissingPreviewJobs(ctx, pgtype.Timestamptz{
			Time: time.Now().Add(options.queue().Repair().Delay), Valid: true,
		})
	}
	return dispatcher
}

func newDispatcher(
	store storeQueries, queries importQueries, catalog Catalog, runnerAPI runnerAPI, files *Files, options Options,
) *Dispatcher {
	if options.PollInterval <= 0 {
		options.PollInterval = 3 * time.Second
	}
	imports := newImporter(runnerAPI, catalog, files, options.Encoders)
	queue := outbox.New[db.ClaimDispatchJobsRow](
		"preview", &jobStore{queries: store, catalog: catalog, progress: options.Progress, queue: options.queue()},
		&handler{runner: runnerAPI, queries: queries, catalog: catalog, imports: imports, pollInterval: options.PollInterval},
		outbox.Policy{MaxRetries: maxRetries, Base: 5 * time.Second, Max: 5 * time.Minute, Lease: leaseDuration},
		outbox.Options{PollInterval: options.PollInterval, BatchSize: options.BatchSize},
	)
	imports.done = queue.Nudge
	return &Dispatcher{queue: queue, imports: imports}
}

// queue is the render queue policy, defaults when unset.
func (o Options) queue() renderqueue.Policy {
	if o.Queue == (renderqueue.Policy{}) {
		return renderqueue.Defaults()
	}
	return o.Queue
}

// Nudge asks the dispatch loop to poll now. It never blocks, so request
// handlers call it after committing new intents instead of dispatching
// themselves.
func (d *Dispatcher) Nudge() { d.queue.Nudge() }

// Run polls until ctx is canceled, then waits for the running imports (which
// the same cancellation stops), so nothing writes after it returns.
func (d *Dispatcher) Run(ctx context.Context) {
	d.queue.Run(ctx)
	d.imports.running.Wait()
}

// Reconcile is the housekeeping the maintenance schedule runs every minute:
// the preview file sweep, the storage budget and the safety net for a lost
// intent (intents are written transactionally with the revision, so the last
// only repairs loss). It wakes the poll loop when it finished.
func (d *Dispatcher) Reconcile(ctx context.Context) error {
	if d.reconcile != nil {
		if err := d.reconcile(ctx); err != nil {
			return err
		}
	}
	d.queue.Nudge()
	return nil
}

// PollOnce claims and processes due phase intents once.
func (d *Dispatcher) PollOnce(ctx context.Context) error {
	return d.queue.PollOnce(ctx)
}
