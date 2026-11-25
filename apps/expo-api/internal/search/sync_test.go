package search

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// fakeRows is a Syncer source and query set without PostgreSQL: one listed
// entry (or none, with entryErr unset and found false) and its leaf stats.
type fakeRows struct {
	entry    catalog.Entry
	found    bool
	entryErr error
	stats    []db.BlueprintStat
}

func (f *fakeRows) ListedEntry(context.Context, string) (catalog.Entry, bool, error) {
	return f.entry, f.found, f.entryErr
}
func (f *fakeRows) ListBlueprintStatsByRevision(context.Context, string) ([]db.BlueprintStat, error) {
	return f.stats, nil
}
func (f *fakeRows) ListSearchableEntryIDs(context.Context) ([]string, error) { return nil, nil }
func (f *fakeRows) EnqueueSearchIndexJob(context.Context, string) error      { return nil }

type fakeEngine struct {
	written []projection
	removed []string
}

func (e *fakeEngine) writeProjection(_ context.Context, p projection) error {
	e.written = append(e.written, p)
	return nil
}

func (e *fakeEngine) removeEntry(_ context.Context, id string) error {
	e.removed = append(e.removed, id)
	return nil
}

const testEntryID = "111111111111"

func publicRows() *fakeRows {
	leaf := catalog.Node{
		Kind: "blueprint", ID: "000000000002", Label: "Smelter", EntityCount: 4,
		PreviewStatus: catalog.PreviewReady, Preview: &catalog.Preview{},
	}
	return &fakeRows{
		found: true,
		entry: catalog.Entry{
			ID: "000000000001", Visibility: "public", Title: "Smelter", Tags: []string{},
			Revision: &catalog.Revision{ID: "000000000003", GameVersion: "2.0.0", Root: &leaf},
		},
	}
}

func TestSyncerWritesPublicEntryWithoutPostgres(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	rows := publicRows()
	rows.entry.ViewCount = 42
	syncer := &Syncer{source: rows, queries: rows, engine: eng}
	if err := syncer.IndexEntry(context.Background(), testEntryID); err != nil {
		t.Fatal(err)
	}
	if len(eng.written) != 1 || len(eng.removed) != 0 {
		t.Fatalf("written %d removed %v", len(eng.written), eng.removed)
	}
	doc := eng.written[0]
	if doc.Entry.GameVersion != "2.0.0" || doc.Entry.LeafCount != 1 || doc.Entry.EntityCount != 4 ||
		doc.Entry.ViewCount != 42 || doc.Entry.PreviewCount != 1 || doc.Entry.RootKind != "blueprint" ||
		len(doc.Blueprints) != 1 || !doc.Blueprints[0].HasPreview {
		t.Fatalf("unexpected projection: %+v", doc.Entry)
	}
}

// An entry that is not listed (private, a quick preview, deleted, expired or
// missing) is removed from the index.
func TestSyncerRemovesUnlistedEntries(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	queries := &fakeRows{}
	if err := (&Syncer{source: queries, queries: queries, engine: eng}).IndexEntry(context.Background(), testEntryID); err != nil {
		t.Fatal(err)
	}
	if len(eng.written) != 0 || len(eng.removed) != 1 || eng.removed[0] != testEntryID {
		t.Fatalf("written %d removed %v", len(eng.written), eng.removed)
	}
}

// A stored snapshot that no longer decodes leaves the leaf's production out
// (the leaf detail shows it as failed) instead of keeping the entry out of
// search.
func TestProjectionSkipsUndecodableProduction(t *testing.T) {
	t.Parallel()
	rows := publicRows()
	stats := []db.BlueprintStat{{
		BlueprintID: "000000000002", Production: []byte("not a snapshot"),
		ProductionSchemaVersion: pgtype.Int4{Int32: 1, Valid: true},
		ProductionState:         pgtype.Text{String: "complete", Valid: true},
	}}
	proj, err := projectEntry(entryRows{Entry: rows.entry, Stats: stats})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if !proj.Indexable || len(proj.Entry.ProductItems) != 0 || len(proj.Blueprints) != 1 {
		t.Fatalf("projection = %+v", proj.Entry)
	}
}
