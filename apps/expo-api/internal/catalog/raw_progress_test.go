package catalog_test

import (
	"context"
	"errors"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// The raw strings are exactly what was submitted (a leaf's own string for a
// single blueprint), keyed by the immutable row they came from, and a missing
// or malformed id is not found.
func TestRawStringsAreTheSubmittedBytes(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	exchange := readFixture(t, "bp-starter-smelter.txt")
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, Visibility: "public"})
	if err != nil {
		t.Fatal(err)
	}
	leaf := entryLeaves(t, pool, entry.ID)[0]

	live, err := service.LiveRevisionRaw(ctx, entry.ID)
	if err != nil || live.ExchangeString != exchange || live.SourceID != entry.Revision.ID {
		t.Fatalf("live revision raw = %+v, %v", live, err)
	}
	revision, err := service.RevisionRaw(ctx, entry.ID, entry.Revision.ID)
	if err != nil || revision.ExchangeString != exchange || revision.SourceID != entry.Revision.ID {
		t.Fatalf("revision raw = %+v, %v", revision, err)
	}
	blueprint, err := service.BlueprintRaw(ctx, leaf)
	if err != nil || blueprint.ExchangeString == "" || blueprint.SourceID != leaf {
		t.Fatalf("blueprint raw = %+v, %v", blueprint, err)
	}

	if _, err := service.RevisionRaw(ctx, entry.ID, "AAAAAAAAAAAA"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown revision: %v", err)
	}
	if _, err := service.BlueprintRaw(ctx, "not an id"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("malformed blueprint id: %v", err)
	}
	if _, err := service.LiveRevisionRaw(ctx, "AAAAAAAAAAAA"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown entry: %v", err)
	}
}

// The progress snapshot lists the live revision's leaves with both phases
// pending until a worker finishes them, and an unreadable entry is not found.
func TestEntryProgressAndLeafPreviews(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Visibility: "public"})
	if err != nil {
		t.Fatal(err)
	}
	leaf := entryLeaves(t, pool, entry.ID)[0]

	snapshot, err := service.EntryProgress(ctx, entry.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.EntryID != entry.ID || snapshot.RevisionID != entry.Revision.ID || len(snapshot.Leaves) != 1 ||
		snapshot.Leaves[0].BlueprintID != leaf || snapshot.Done {
		t.Fatalf("progress = %+v, want one unsettled leaf of the live revision", snapshot)
	}
	if _, err := service.EntryProgress(ctx, "AAAAAAAAAAAA", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown entry: %v", err)
	}

	previews, err := service.LeafPreviews(ctx, []string{leaf})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := previews[leaf]; !ok || got.PreviewURL != "" || !got.PreviewPending {
		t.Fatalf("leaf previews = %+v, want the leaf rendering", previews)
	}
	if _, err := service.LeafPreviews(ctx, []string{"not an id"}); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("malformed leaf id: %v", err)
	}
}
