package search

import (
	"context"
	"errors"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// racingIndexer runs during while the dispatcher has the entry's job claimed,
// as a concurrent delete, edit or visibility change would.
type racingIndexer struct {
	during func(ctx context.Context) error
	err    error
}

func (r *racingIndexer) IndexEntry(ctx context.Context, _ string) error {
	if r.during != nil {
		if err := r.during(ctx); err != nil {
			return err
		}
	}
	return r.err
}

func (r *racingIndexer) DeleteEntry(context.Context, string) error { return nil }

func TestSyncedJobIsResolvedAsClaimed(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()
	if _, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readProjectionFixture(t, "bp-starter-smelter.txt"), Visibility: "public",
	}); err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if err := New(pool, &racingIndexer{}, Options{}).PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM entry_search_jobs").Scan(&left); err != nil || left != 0 {
		t.Fatalf("search jobs left after a clean sync = %d (%v), want 0", left, err)
	}

	// A failed sync of an unchanged row spends one retry.
	if _, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readProjectionFixture(t, "bp-starter-smelter.txt"), Visibility: "public",
	}); err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if err := New(pool, &racingIndexer{err: errors.New("typesense down")}, Options{}).PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var retries int32
	if err := pool.QueryRow(ctx, "SELECT retry_count FROM entry_search_jobs").Scan(&retries); err != nil || retries != 1 {
		t.Fatalf("retry_count after a failed sync = %d (%v), want 1", retries, err)
	}
}

func TestReEnqueueDuringSyncIsNotLost(t *testing.T) {
	for _, test := range []struct {
		name    string
		syncErr error
	}{
		{name: "sync succeeds"},
		{name: "sync fails", syncErr: errors.New("typesense down")},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool, service := newProjectionPool(t)
			ctx := context.Background()
			entry, err := service.CreateEntry(ctx, catalog.Submission{
				Exchange: readProjectionFixture(t, "bp-starter-smelter.txt"), Visibility: "public",
			})
			if err != nil {
				t.Fatalf("create entry: %v", err)
			}
			entryID, err := parseID(entry.ID)
			if err != nil {
				t.Fatal(err)
			}
			queries := db.New(pool)
			// The entry is deleted while its index job is in flight: the index
			// write lands first, so only the queued delete can remove it again.
			indexer := &racingIndexer{err: test.syncErr, during: func(ctx context.Context) error {
				return queries.EnqueueSearchDeleteJob(ctx, entryID)
			}}
			if err := New(pool, indexer, Options{}).PollOnce(ctx); err != nil {
				t.Fatal(err)
			}

			var action, state, code string
			var retries int32
			if err := pool.QueryRow(ctx,
				"SELECT action, state, retry_count, failure_code FROM entry_search_jobs WHERE entry_id = $1", entryID,
			).Scan(&action, &state, &retries, &code); err != nil {
				t.Fatalf("queued delete was lost: %v", err)
			}
			if action != actionDelete || state != "pending" || retries != 0 || code != "" {
				t.Fatalf("job after racing delete = %s / %s / %d / %q, want a fresh pending delete",
					action, state, retries, code)
			}
		})
	}
}
