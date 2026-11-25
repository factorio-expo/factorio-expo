package catalog_test

import (
	"context"
	"sync"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// A listing change brings every effect with it, whatever the write path:
// the owner's upload award once listed (once per entry), and the edge-cache
// purge only when the entry just became private.
func TestListingChangeEffects(t *testing.T) {
	var mu sync.Mutex
	hidden := map[string]int{}
	service, pool := newServiceWith(t, catalog.Options{EntryHidden: func(entryID string, _, _ []string) {
		mu.Lock()
		defer mu.Unlock()
		hidden[entryID]++
	}})
	ctx := context.Background()
	const owner = "listingOwner"
	if _, err := pool.Exec(ctx, "TRUNCATE users CASCADE"); err != nil {
		t.Fatalf("truncate users: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO users (id, username) VALUES ($1, 'listing-owner')", owner); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	auth := catalog.Authorization{UserID: owner}
	uploads := func() int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM xp_events WHERE user_id = $1 AND reason = 'upload'", owner).Scan(&n); err != nil {
			t.Fatalf("count awards: %v", err)
		}
		return n
	}
	setVisibility := func(entryID string, visibility catalog.Visibility) {
		t.Helper()
		if _, err := service.UpdateEntryMetadata(ctx, entryID, auth, catalog.EntryMetadataUpdate{Visibility: &visibility}); err != nil {
			t.Fatalf("set %s: %v", visibility, err)
		}
	}

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: owner, Visibility: "unlisted",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if got := uploads(); got != 0 {
		t.Fatalf("awards of an unlisted upload = %d, want 0", got)
	}
	setVisibility(entry.ID, catalog.VisibilityPublic)
	setVisibility(entry.ID, catalog.VisibilityUnlisted)
	setVisibility(entry.ID, catalog.VisibilityPublic)
	if got := uploads(); got != 1 {
		t.Fatalf("awards after listing twice = %d, want 1", got)
	}
	if hidden[entry.ID] != 0 {
		t.Fatalf("purges before going private = %d, want 0", hidden[entry.ID])
	}

	setVisibility(entry.ID, catalog.VisibilityPrivate)
	title := "Still private"
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, auth, catalog.EntryMetadataUpdate{Title: &title}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	setVisibility(entry.ID, catalog.VisibilityPrivate)
	mu.Lock()
	defer mu.Unlock()
	if hidden[entry.ID] != 1 {
		t.Fatalf("purges = %d, want 1: only the change to private", hidden[entry.ID])
	}
	assertSearchJob(t, pool, entry.ID, "index")
}
