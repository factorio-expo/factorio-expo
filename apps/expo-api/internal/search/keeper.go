package search

import (
	"context"
	"log/slog"
	"time"
)

// Keeper owns the search index while the API runs, so the API serves
// requests before the index is built. It builds this version's index when it
// is not live (EnsureIndex; searches keep reading the published version
// meanwhile), runs the entry_search_jobs dispatcher only while the index is
// live, and checks it every CheckInterval: an index Typesense lost (alias or
// collection gone, or fewer or more documents than PostgreSQL says while no
// search intent is outstanding) is rebuilt with the dispatcher stopped.
// While the index is live it also sends changed view counts every
// ViewInterval (refreshViews).
type Keeper struct {
	index      *TypesenseClient
	syncer     *Syncer
	dispatcher *Dispatcher
	counts     keeperQueries
	options    KeeperOptions
}

// keeperQueries are the PostgreSQL counts the periodic check compares and
// the view counts the view refresh sends.
type keeperQueries interface {
	CountSearchableDocuments(ctx context.Context) (int64, error)
	CountSearchJobs(ctx context.Context) (int64, error)
	viewQueries
}

// KeeperOptions configures a Keeper.
type KeeperOptions struct {
	// CheckInterval is how often the live index is checked (default 1 h).
	CheckInterval time.Duration
	// ViewInterval is how often changed view counts are sent (default 10 min).
	ViewInterval time.Duration
	// RetryBase and RetryMax bound the backoff after a failed build
	// (default 5 s doubling to 5 min).
	RetryBase, RetryMax time.Duration
}

// NewKeeper builds a Keeper; counts is usually the generated queries.
func NewKeeper(index *TypesenseClient, syncer *Syncer, dispatcher *Dispatcher, counts keeperQueries, options KeeperOptions) *Keeper {
	if options.CheckInterval <= 0 {
		options.CheckInterval = time.Hour
	}
	if options.ViewInterval <= 0 {
		options.ViewInterval = 10 * time.Minute
	}
	if options.RetryBase <= 0 {
		options.RetryBase = 5 * time.Second
	}
	if options.RetryMax <= 0 {
		options.RetryMax = 5 * time.Minute
	}
	return &Keeper{index: index, syncer: syncer, dispatcher: dispatcher, counts: counts, options: options}
}

// Run keeps the index live until ctx is canceled.
func (k *Keeper) Run(ctx context.Context) {
	build := k.ensure
	for ctx.Err() == nil {
		if !k.untilBuilt(ctx, build) {
			return
		}
		k.serve(ctx)
		build = k.rebuild
	}
}

// ensure is the startup build: nothing to do when the index is live.
func (k *Keeper) ensure(ctx context.Context) error {
	rebuilt, indexed, err := k.index.EnsureIndex(ctx, k.syncer)
	if err != nil {
		return err
	}
	if rebuilt {
		slog.Info("search index rebuilt", "version", k.index.IndexVersion(), "entries", indexed)
	} else {
		slog.Info("search index is current", "version", k.index.IndexVersion())
	}
	return nil
}

// rebuild rebuilds after a failed check: the index is broken, so it is
// rebuilt even when its alias is still in place.
func (k *Keeper) rebuild(ctx context.Context) error {
	indexed, err := k.index.Rebuild(ctx, k.syncer)
	if err == nil {
		slog.Info("search index rebuilt", "version", k.index.IndexVersion(), "entries", indexed)
	}
	return err
}

// untilBuilt retries build with capped exponential backoff until it succeeds
// (true) or ctx is canceled (false).
func (k *Keeper) untilBuilt(ctx context.Context, build func(context.Context) error) bool {
	delay := k.options.RetryBase
	for {
		err := build(ctx)
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		slog.Error("build search index", "error", err, "retry_in", delay)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(delay):
		}
		delay = min(delay*2, k.options.RetryMax)
	}
}

// serve runs the dispatcher until ctx is canceled or a check finds the index
// broken; the dispatcher has stopped when it returns.
func (k *Keeper) serve(ctx context.Context) {
	dispatchCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		k.dispatcher.Run(dispatchCtx)
	}()
	defer func() {
		stop()
		<-done
	}()

	ticker := time.NewTicker(k.options.CheckInterval)
	defer ticker.Stop()
	viewTicker := time.NewTicker(k.options.ViewInterval)
	defer viewTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-viewTicker.C:
			if err := refreshViews(ctx, k.counts, k.index); err != nil && ctx.Err() == nil {
				slog.Warn("refresh search view counts", "error", err)
			}
		case <-ticker.C:
			if reason := k.broken(ctx); reason != "" {
				slog.Warn("search index is broken; rebuilding", "reason", reason)
				return
			}
		}
	}
}

// broken says why the live index needs a rebuild, or "" when it is fine or
// the check cannot tell (an unreachable Typesense or database is left to the
// next check; the dispatcher's retries cover the outage itself).
func (k *Keeper) broken(ctx context.Context) string {
	live, _, err := k.index.indexLive(ctx)
	if err != nil {
		slog.Warn("check search index", "error", err)
		return ""
	}
	if !live {
		return "index is not live"
	}
	// Outstanding intents (queued, in flight or failed) make the index differ
	// from PostgreSQL legitimately; compare only when none exist before and
	// after counting.
	if !k.quiet(ctx) {
		return ""
	}
	want, err := k.counts.CountSearchableDocuments(ctx)
	if err != nil {
		slog.Warn("check search index", "error", err)
		return ""
	}
	have, exists, err := k.index.documentCount(ctx)
	if err != nil {
		slog.Warn("check search index", "error", err)
		return ""
	}
	if !k.quiet(ctx) {
		return ""
	}
	if !exists {
		return "collection is gone"
	}
	if have != want {
		slog.Warn("search index document count differs", "indexed", have, "searchable", want)
		return "document count differs from PostgreSQL"
	}
	return ""
}

// quiet reports whether entry_search_jobs is empty (false when unknown).
func (k *Keeper) quiet(ctx context.Context) bool {
	jobs, err := k.counts.CountSearchJobs(ctx)
	if err != nil {
		slog.Warn("check search index", "error", err)
		return false
	}
	return jobs == 0
}
