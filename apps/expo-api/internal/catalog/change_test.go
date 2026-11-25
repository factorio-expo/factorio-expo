package catalog_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/progress"
)

// Entry writes tell open streams and the dispatcher after they commit, never
// for a write that rolled back.
func TestEntryChangesNotifyAfterCommit(t *testing.T) {
	hub := progress.NewHub()
	var nudges atomic.Int32
	service, pool := newServiceWith(t, catalog.Options{Progress: hub, WorkQueued: func() { nudges.Add(1) }})
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if nudges.Load() != 1 {
		t.Fatalf("nudges after an upload = %d, want 1", nudges.Load())
	}
	sub := hub.Subscribe(entry.ID)
	defer sub.Close()
	auth := catalog.Authorization{EditToken: entry.EditToken}

	blank := " "
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Title: &blank}); !errors.Is(err, catalog.ErrInvalidEntryMetadata) {
		t.Fatalf("blank title error = %v, want ErrInvalidEntryMetadata", err)
	}
	if len(sub.Changes()) != 0 {
		t.Fatal("a rejected edit notified the entry's streams")
	}

	title := "Renamed"
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Title: &title}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := service.DeleteEntry(ctx, entry.ID, auth); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, step := range []string{"edit", "delete"} {
		select {
		case change := <-sub.Changes():
			if change != (progress.Change{EntryID: entry.ID}) {
				t.Fatalf("%s change = %+v, want an entry change", step, change)
			}
		default:
			t.Fatalf("%s did not notify the entry's streams", step)
		}
	}
	assertSearchJob(t, pool, entry.ID, "delete")
	if nudges.Load() != 1 {
		t.Fatalf("nudges = %d, want only the upload's", nudges.Load())
	}
}

// A stored preview or production queues the entry's search sync in the same
// transaction; files no row takes (a repeated store, a deleted blueprint) are
// queued for the file sweep instead.
func TestImportsQueueSearchSync(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	leaf := entry.Revision.Root.Leaves()[0].ID
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatalf("drain search jobs: %v", err)
	}
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := pool.Exec(ctx, "DELETE FROM preview_file_deletions WHERE source_sha256 = $1", sha); err != nil {
		t.Fatal(err)
	}
	queuedFiles := func() (n int) {
		t.Helper()
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM preview_file_deletions WHERE source_sha256 = $1", sha).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for i, wantQueued := range []int{0, 1} {
		if err := service.StorePreview(ctx, leaf, sha, 64, 64); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
		if got := queuedFiles(); got != wantQueued {
			t.Fatalf("after store %d, queued files = %d, want %d", i, got, wantQueued)
		}
	}
	assertSearchJob(t, pool, entry.ID, "index")
	if err := service.StorePreview(ctx, "nosuchleaf01", sha, 64, 64); err != nil {
		t.Fatalf("store for a missing blueprint: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatalf("drain search jobs: %v", err)
	}
	if err := service.AnalysisFailed(ctx, leaf, "analysis_failed", "detail"); err != nil {
		t.Fatalf("analysis failed: %v", err)
	}
	assertSearchJob(t, pool, entry.ID, "index")

	// A deleted entry is never brought back by a late import.
	if err := service.DeleteEntry(ctx, entry.ID, catalog.Authorization{EditToken: entry.EditToken}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := service.AnalysisFailed(ctx, leaf, "analysis_failed", "detail"); err != nil {
		t.Fatalf("analysis failed after delete: %v", err)
	}
	assertSearchJob(t, pool, entry.ID, "delete")
}
