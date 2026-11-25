package views

import (
	"context"
	"log/slog"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// DefaultFlushInterval is how often counted views reach PostgreSQL; a crash
// loses at most this much.
const DefaultFlushInterval = time.Minute

// Store is where flushed views go; the generated queries implement it.
type Store interface {
	AddEntryViews(ctx context.Context, arg db.AddEntryViewsParams) error
}

// Flusher adds a Counter's views to the Store on an interval.
type Flusher struct {
	counter  *Counter
	store    Store
	interval time.Duration
}

// NewFlusher builds a Flusher; interval <= 0 takes DefaultFlushInterval.
func NewFlusher(counter *Counter, store Store, interval time.Duration) *Flusher {
	if interval <= 0 {
		interval = DefaultFlushInterval
	}
	return &Flusher{counter: counter, store: store, interval: interval}
}

// Run flushes every interval until ctx is canceled, then once more so a
// graceful shutdown keeps the last interval's views.
func (f *Flusher) Run(ctx context.Context) {
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			f.Flush(final)
			return
		case <-ticker.C:
			f.Flush(ctx)
		}
	}
}

// Flush writes the pending views in one statement. On failure they go back
// to the counter for the next attempt.
func (f *Flusher) Flush(ctx context.Context) {
	if f.counter.takeFull() {
		slog.Warn("view counter is full for today; new visitors are not counted until tomorrow")
	}
	pending := f.counter.Take()
	if len(pending) == 0 {
		return
	}
	params := db.AddEntryViewsParams{
		EntryIds: make([]string, 0, len(pending)),
		Views:    make([]int64, 0, len(pending)),
	}
	for id, n := range pending {
		params.EntryIds = append(params.EntryIds, id)
		params.Views = append(params.Views, n)
	}
	if err := f.store.AddEntryViews(ctx, params); err != nil {
		slog.Error("flush entry views", "entries", len(pending), "error", err)
		f.counter.Restore(pending)
	}
}
