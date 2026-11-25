package search

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// keeperCounts is the PostgreSQL side of the periodic check.
type keeperCounts struct {
	searchable, jobs int64
	err              error
}

func (c *keeperCounts) CountSearchableDocuments(context.Context) (int64, error) {
	return c.searchable, c.err
}

func (c *keeperCounts) CountSearchJobs(context.Context) (int64, error) { return c.jobs, c.err }

func (c *keeperCounts) ListUnindexedViewCounts(context.Context, int32) ([]db.ListUnindexedViewCountsRow, error) {
	return nil, c.err
}

func (c *keeperCounts) MarkViewCountsIndexed(context.Context, db.MarkViewCountsIndexedParams) error {
	return c.err
}

func TestKeeperCheckFindsALostOrDivergedIndex(t *testing.T) {
	for _, test := range []struct {
		name    string
		setup   func(*fakeTypesense, collectionNames)
		counts  keeperCounts
		broken  bool
		because string
	}{
		{name: "complete", counts: keeperCounts{searchable: 3}},
		{
			name: "alias lost", counts: keeperCounts{searchable: 3}, broken: true, because: "not live",
			setup: func(f *fakeTypesense, _ collectionNames) { delete(f.aliases, "blueprints") },
		},
		{
			name: "collection lost", counts: keeperCounts{searchable: 3}, broken: true, because: "not live",
			setup: func(f *fakeTypesense, n collectionNames) { delete(f.collections, n.Blueprints) },
		},
		{name: "documents missing", counts: keeperCounts{searchable: 5}, broken: true, because: "count"},
		// Queued intents explain a difference; the dispatcher will close it.
		{name: "intents outstanding", counts: keeperCounts{searchable: 5, jobs: 2}},
		// A check that cannot count does not rebuild.
		{name: "database down", counts: keeperCounts{searchable: 5, err: errors.New("down")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypesense()
			client := newFakeClient(t, fake)
			fake.live(client.names)
			fake.documents[client.names.Blueprints] = 3
			if test.setup != nil {
				test.setup(fake, client.names)
			}
			keeper := NewKeeper(client, nil, nil, &test.counts, KeeperOptions{})

			reason := keeper.broken(context.Background())
			if (reason != "") != test.broken || !strings.Contains(reason, test.because) {
				t.Fatalf("broken = %q, want broken %v (%q)", reason, test.broken, test.because)
			}
		})
	}
}

// Run starts the dispatcher only once the index is live, and when a check
// finds the index lost it stops the dispatcher and rebuilds.
func TestKeeperRebuildsALostIndex(t *testing.T) {
	fake := newFakeTypesense()
	client := newFakeClient(t, fake)
	fake.live(client.names)
	built := make(chan struct{}, 4)
	syncer := &Syncer{engine: client, queries: &listingQueries{list: func() { built <- struct{}{} }}}
	jobs := &countingJobs{}
	dispatcher := newDispatcher(jobs, syncer, Options{PollInterval: time.Millisecond})
	keeper := NewKeeper(client, syncer, dispatcher, &keeperCounts{}, KeeperOptions{CheckInterval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		keeper.Run(ctx)
	}()
	defer func() {
		cancel()
		<-stopped
	}()

	// The index was live, so the dispatcher runs without a build.
	waitFor(t, "the dispatcher to poll", func() bool { return jobs.claims() > 0 })
	select {
	case <-built:
		t.Fatal("a live index was rebuilt")
	default:
	}

	fake.mu.Lock()
	delete(fake.aliases, "blueprints")
	fake.mu.Unlock()
	select {
	case <-built:
	case <-time.After(5 * time.Second):
		t.Fatal("a lost index was not rebuilt")
	}
	waitFor(t, "the index to be published again", func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.aliases["blueprints"] == client.names.Blueprints
	})
}

// countingJobs is an empty entry_search_jobs table that counts claims.
type countingJobs struct {
	searchQueries
	mu    sync.Mutex
	count int
}

func (j *countingJobs) ClaimSearchJobs(context.Context, db.ClaimSearchJobsParams) ([]db.EntrySearchJob, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.count++
	return nil, nil
}

func (j *countingJobs) claims() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.count
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}
