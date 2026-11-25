package search

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

// The Postgres-backed tests in this package (this file, dispatcher_sql_test.go)
// truncate tables other packages' tests use too; run the module's tests with
// -p 1 against TEST_DATABASE_URL (README).

func newProjectionPool(t *testing.T) (*pgxpool.Pool, *catalog.Service) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(),
		"TRUNCATE users, entries, entry_search_jobs, entry_tags CASCADE",
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool, catalog.New(pool, catalog.Options{})
}

func readProjectionFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../test-data/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestBuildProjectionSingleLeaf(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()
	title := "Uploader title"
	description := "Uploader description for search."

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readProjectionFixture(t, "bp-starter-smelter.txt"), Visibility: "public",
		Title: &title, Description: &description,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	entryID, err := parseID(entry.ID)
	if err != nil {
		t.Fatalf("parse entry id: %v", err)
	}
	proj, err := buildProjection(ctx, service, db.New(pool), entryID)
	if err != nil {
		t.Fatalf("build projection: %v", err)
	}
	if !proj.Indexable {
		t.Fatal("expected a public entry with a current revision to be indexable")
	}
	if proj.Entry.ID != entry.ID {
		t.Fatalf("entry id = %q, want %q", proj.Entry.ID, entry.ID)
	}
	if proj.Entry.RootKind != "blueprint" {
		t.Fatalf("root_kind = %q, want blueprint", proj.Entry.RootKind)
	}
	if proj.Entry.Title != title || proj.Entry.Description != description {
		t.Fatalf("indexed title/description = (%q, %q), want (%q, %q)",
			proj.Entry.Title, proj.Entry.Description, title, description)
	}
	if proj.Entry.SearchText == "" {
		t.Fatal("expected root node and entry metadata in search text")
	}
	if len(proj.Entry.Icons) == 0 {
		t.Fatal("expected the leaf's icons to be split into tokens")
	}
	if len(proj.Blueprints) != 1 {
		t.Fatalf("blueprint documents = %d, want 1", len(proj.Blueprints))
	}
	leaf := proj.Blueprints[0]
	if leaf.EntryID != entry.ID {
		t.Fatalf("blueprint entry_id = %q, want %q", leaf.EntryID, entry.ID)
	}
	// The entry is its one blueprint: the card shows the entry's text, and
	// the entry's text is matched as the leaf's own.
	if leaf.Kind != "blueprint" || leaf.NodeID == "" || leaf.ID != leaf.NodeID || leaf.Title != title ||
		leaf.Description != description || leaf.BookTitle != "" {
		t.Fatalf("single leaf document = %+v", leaf)
	}
	if !strings.Contains(leaf.SearchText, "Uploader title") ||
		leaf.GameVersion != proj.Entry.GameVersion || leaf.LeafCount != 1 ||
		!slices.Equal(leaf.VersionGroups, proj.Entry.VersionGroups) ||
		len(leaf.VersionGroups) == 0 && leaf.GameVersion != "" {
		t.Fatalf("single leaf document = %+v", leaf)
	}
	if proj.Entry.LeafCount != int64(1) {
		t.Fatalf("leaf_count = %d, want 1", proj.Entry.LeafCount)
	}
	revisionID, err := parseID(entry.Revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	blueprints, err := db.New(pool).ListBlueprintsByRevision(ctx, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(blueprints) != 1 || proj.Entry.EntityCount != int64(blueprints[0].EntityCount) {
		t.Fatalf("entity_count = %d, want decoded leaf count", proj.Entry.EntityCount)
	}
}

func TestBuildProjectionBookAggregatesLeaves(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readProjectionFixture(t, "book-starter-kit.txt"), Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	entryID, err := parseID(entry.ID)
	if err != nil {
		t.Fatalf("parse entry id: %v", err)
	}
	proj, err := buildProjection(ctx, service, db.New(pool), entryID)
	if err != nil {
		t.Fatalf("build projection: %v", err)
	}
	if !proj.Indexable {
		t.Fatal("expected a public entry with a current revision to be indexable")
	}
	if proj.Entry.RootKind != "book" {
		t.Fatalf("root_kind = %q, want book", proj.Entry.RootKind)
	}
	// The book itself, then each of its blueprints.
	if int64(len(proj.Blueprints)) != proj.Entry.LeafCount+1 || proj.Entry.LeafCount == 0 {
		t.Fatalf("documents = %d, leaf_count = %d", len(proj.Blueprints), proj.Entry.LeafCount)
	}
	// The keeper's document count agrees with the projection.
	if want, err := db.New(pool).CountSearchableDocuments(ctx); err != nil || want != int64(len(proj.Blueprints)) {
		t.Fatalf("CountSearchableDocuments = %d, %v; want %d", want, err, len(proj.Blueprints))
	}
	book := proj.Blueprints[0]
	if book.ID != bookDocumentID(entry.ID) || book.Kind != "book" || book.NodeID != "" ||
		book.Position != -1 || book.SearchText != proj.Entry.SearchText ||
		book.EntityCount != proj.Entry.EntityCount || len(book.rates) != len(proj.Entry.rates) {
		t.Fatalf("book document = %+v", book)
	}
	revisionID, err := parseID(entry.Revision.ID)
	if err != nil {
		t.Fatal(err)
	}
	blueprints, err := db.New(pool).ListBlueprintsByRevision(ctx, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	var entityCount int64
	for _, bp := range blueprints {
		entityCount += int64(bp.EntityCount)
	}
	if proj.Entry.EntityCount != entityCount {
		t.Fatalf("entity_count = %d, want %d", proj.Entry.EntityCount, entityCount)
	}
	// Every leaf document carries its entry id (to replace or remove an
	// entry's documents), the book's title for the card (not for matching:
	// its text is the leaf's own) and its place in the book.
	rows := make(map[string]db.Blueprint, len(blueprints))
	for _, bp := range blueprints {
		rows[bp.ID] = bp
	}
	for i, doc := range proj.Blueprints[1:] {
		row, ok := rows[doc.NodeID]
		if doc.EntryID != entry.ID || !ok || doc.Position != int64(row.Position) {
			t.Fatalf("leaf %d: entry_id %q, node_id %q, position %d", i, doc.EntryID, doc.NodeID, doc.Position)
		}
		if doc.BookTitle == "" || doc.SearchText != leafSearchText(row) || doc.Kind != "blueprint" ||
			doc.Title != row.Label {
			t.Fatalf("leaf %d document = %+v", i, doc)
		}
	}
}

// A book of planners is a book result, and each planner, sub-books
// included, a result of its own that names its book.
func TestBuildProjectionBookOfPlanners(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readProjectionFixture(t, "planner-upgrade-set.txt"), Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	proj, err := buildProjection(ctx, service, db.New(pool), entry.ID)
	if err != nil {
		t.Fatalf("build projection: %v", err)
	}
	// The book and its 7 planners: 3 at the top, 4 in its 2 sub-books.
	if len(proj.Blueprints) != 8 {
		t.Fatalf("documents = %d, want the book and every planner", len(proj.Blueprints))
	}
	book := proj.Blueprints[0]
	if book.ID != bookDocumentID(entry.ID) || book.Kind != "book" || book.EntryID != entry.ID ||
		book.SearchText != proj.Entry.SearchText || !strings.Contains(book.Title, "Upgrade set") {
		t.Fatalf("book document = %+v", book)
	}
	for i, doc := range proj.Blueprints[1:] {
		if doc.Kind != "upgrade_planner" || doc.NodeID == "" || doc.ID != doc.NodeID ||
			doc.Position != int64(i) || doc.BookTitle != book.Title || doc.EntryID != entry.ID ||
			len(doc.EntityNames) != 0 || doc.FootprintShort != nil {
			t.Fatalf("planner %d document = %+v", i, doc)
		}
	}
}

// An entry that is one planner is a single planner result with the entry's
// text.
func TestBuildProjectionSinglePlanner(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()
	title := "Cliff remover"

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readProjectionFixture(t, "planner-upgrade-set.txt"), Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	var plannerID string
	for _, child := range entry.Revision.Root.Children {
		if child.Kind == "upgrade_planner" {
			plannerID = child.ID
			break
		}
	}
	exchange, err := service.ExportNodeString(ctx, plannerID, "")
	if err != nil {
		t.Fatalf("export planner: %v", err)
	}
	single, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: exchange, Visibility: "public", Title: &title,
	})
	if err != nil {
		t.Fatalf("create planner entry: %v", err)
	}
	proj, err := buildProjection(ctx, service, db.New(pool), single.ID)
	if err != nil {
		t.Fatalf("build projection: %v", err)
	}
	if len(proj.Blueprints) != 1 {
		t.Fatalf("documents = %d, want 1", len(proj.Blueprints))
	}
	doc := proj.Blueprints[0]
	if doc.Kind != "upgrade_planner" || doc.Title != title || doc.BookTitle != "" ||
		!strings.Contains(doc.SearchText, title) {
		t.Fatalf("planner document = %+v", doc)
	}
}

// Only public entries are searchable: unlisted ones are reachable by link alone.
func TestBuildProjectionNonPublicEntryIsNotIndexable(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()
	owner := mustSeedUser(t, pool)

	for _, visibility := range []string{"unlisted", "private"} {
		entry, err := service.CreateEntry(ctx, catalog.Submission{
			Exchange: readProjectionFixture(t, "bp-starter-smelter.txt"), Visibility: visibility, OwnerUserID: owner,
		})
		if err != nil {
			t.Fatalf("create %s entry: %v", visibility, err)
		}

		entryID, err := parseID(entry.ID)
		if err != nil {
			t.Fatalf("parse entry id: %v", err)
		}
		proj, err := buildProjection(ctx, service, db.New(pool), entryID)
		if err != nil {
			t.Fatalf("build projection: %v", err)
		}
		if proj.Indexable {
			t.Fatalf("expected a %s entry to be reported as not indexable", visibility)
		}
	}
}

// mustSeedUser inserts a minimal user row so a private entry's
// owner_user_id foreign key (and its NOT NULL check for private
// visibility) is satisfiable, returning the new user's id.
func mustSeedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (id, username)
		 VALUES ($1, 'search-projection-test')
		 RETURNING id`, shortid.New(),
	).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func TestBuildProjectionDeletedEntryIsNotIndexable(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readProjectionFixture(t, "bp-starter-smelter.txt"), Visibility: "public",
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE entries SET deleted_at = now() WHERE id = $1", entry.ID); err != nil {
		t.Fatal(err)
	}
	entryID, err := parseID(entry.ID)
	if err != nil {
		t.Fatalf("parse entry id: %v", err)
	}
	proj, err := buildProjection(ctx, service, db.New(pool), entryID)
	if err != nil {
		t.Fatalf("build projection: %v", err)
	}
	if proj.Indexable {
		t.Fatal("expected a deleted entry to be reported as not indexable")
	}
}

// A visibility change made through the catalog reaches the index through the
// real sync: the entry is written to Typesense only while public, removed as
// soon as it is unlisted or private, and a full rebuild only picks up public
// entries.
func TestVisibilityChangesReachTheIndex(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()
	owner := mustSeedUser(t, pool)
	queries := db.New(pool)
	visibilities := []string{"public", "unlisted", "private"}

	for _, from := range visibilities {
		for _, to := range visibilities {
			if from == to {
				continue
			}
			entry, err := service.CreateEntry(ctx, catalog.Submission{
				Exchange: readProjectionFixture(t, "bp-starter-smelter.txt"), Visibility: from, OwnerUserID: owner,
			})
			if err != nil {
				t.Fatalf("create %s entry: %v", from, err)
			}
			visibility := catalog.Visibility(to)
			if _, err := service.UpdateEntryMetadata(ctx, entry.ID, catalog.Authorization{UserID: owner},
				catalog.EntryMetadataUpdate{Visibility: &visibility}); err != nil {
				t.Fatalf("%s to %s: %v", from, to, err)
			}

			eng := &fakeEngine{}
			if err := (&Syncer{source: service, queries: queries, engine: eng}).IndexEntry(ctx, entry.ID); err != nil {
				t.Fatalf("%s to %s: index: %v", from, to, err)
			}
			written := len(eng.written) == 1 && len(eng.removed) == 0
			removed := len(eng.written) == 0 && len(eng.removed) == 1 && eng.removed[0] == entry.ID
			if to == "public" && !written || to != "public" && !removed {
				t.Errorf("%s to %s: written %d, removed %v", from, to, len(eng.written), eng.removed)
			}

			ids, err := queries.ListSearchableEntryIDs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			rebuilt := false
			for _, id := range ids {
				rebuilt = rebuilt || id == entry.ID
			}
			if rebuilt != (to == "public") {
				t.Errorf("%s to %s: in a rebuild = %v", from, to, rebuilt)
			}
		}
	}
}

// leafSearchText is the text a leaf row alone gives its document.
func leafSearchText(row db.Blueprint) string {
	title, _ := stripEmbeddedIcons(row.Label)
	description, _ := stripEmbeddedIcons(row.Description)
	return buildSearchText(title, description)
}
