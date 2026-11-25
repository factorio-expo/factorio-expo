package catalog_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/catalog"
)

// TestMain serializes the database-backed packages against each other. Every
// test truncates the catalog tables, so two packages sharing TEST_DATABASE_URL
// would otherwise delete each other's rows mid-test. A session-level advisory
// lock held for the whole binary makes the packages take turns.
func TestMain(m *testing.M) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	lock, err := pool.Acquire(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquire: %v\n", err)
		os.Exit(1)
	}
	defer lock.Release()
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock($1)", catalogTestLockID); err != nil {
		fmt.Fprintf(os.Stderr, "lock: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	_, _ = lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", catalogTestLockID)
	lock.Release()
	pool.Close()
	os.Exit(code)
}

// catalogTestLockID is an arbitrary shared key; any package touching the
// catalog tables in tests must use this same value.
const catalogTestLockID = 8175021

// newService connects to the throwaway Postgres named by TEST_DATABASE_URL and
// truncates the catalog tables, so each test starts from a known empty state.
// Without that variable the test is skipped, keeping `go test ./...` runnable
// without a database.
func newService(t *testing.T) (*catalog.Service, *pgxpool.Pool) {
	t.Helper()
	return newServiceWith(t, catalog.Options{})
}

// newServiceWith is newService with options (a progress hub, say).
func newServiceWith(t *testing.T, options catalog.Options) (*catalog.Service, *pgxpool.Pool) {
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
		"TRUNCATE entries, revisions, books, blueprints, previews, preview_files, phase_jobs, ip_bans, ban_holds CASCADE",
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if options.BanHolds == nil {
		options.BanHolds = testBanHolds
	}
	return catalog.New(pool, options), pool
}

// testBanHolds is the ban hold hasher of the catalog tests.
var testBanHolds = banhold.New("test-ip-secret")

func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../test-data/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(raw)
}

func TestCreateEntryStoresBookTree(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	title := "Community starter book"
	description := "A curated collection of early-game blueprints."

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "book-starter-kit.txt"),
		Title:    &title, Description: &description, Tags: []string{"starter-base", "mid-game"},
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if entry.EditToken == "" {
		t.Fatal("anonymous entry should receive an edit token")
	}
	if entry.Revision == nil || entry.Revision.Root == nil {
		t.Fatal("entry should carry a hydrated revision")
	}
	if entry.Revision.Root.Kind != "book" {
		t.Fatalf("root kind = %q, want book", entry.Revision.Root.Kind)
	}
	if entry.Title != title || entry.Description != description {
		t.Fatalf("entry metadata = (%q, %q), want (%q, %q)",
			entry.Title, entry.Description, title, description)
	}
	if len(entry.Tags) != 2 || entry.Tags[0] != "mid-game" || entry.Tags[1] != "starter-base" {
		t.Fatalf("entry tags = %v, want sorted curated tags", entry.Tags)
	}
	metadataEntry, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil {
		t.Fatalf("reload entry: %v", err)
	}
	if metadataEntry.Title != title || metadataEntry.Description != description || len(metadataEntry.Tags) != 2 {
		t.Fatalf("reloaded entry metadata = %+v", metadataEntry)
	}

	leaves := entry.Revision.Root.Leaves()
	if len(leaves) == 0 {
		t.Fatal("book should contain renderable leaves")
	}
	for _, leaf := range leaves {
		if leaf.ExchangeString == "" {
			t.Fatalf("leaf %s has no self-contained exchange string", leaf.ID)
		}
	}

	// A re-read must reproduce the same tree from stored rows alone.
	reloaded, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil {
		t.Fatalf("get entry: %v", err)
	}
	if got, want := len(reloaded.Revision.Root.Leaves()), len(leaves); got != want {
		t.Fatalf("reloaded leaf count = %d, want %d", got, want)
	}
}

func TestCreateRevisionRequiresAuthorization(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"),
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	if _, err := service.CreateRevision(ctx, entry.ID, catalog.Authorization{},
		catalog.Submission{Exchange: readFixture(t, "book-starter-kit.txt")},
	); err == nil {
		t.Fatal("revision without an edit token should be rejected")
	}

	updated, err := service.CreateRevision(ctx, entry.ID,
		catalog.Authorization{EditToken: entry.EditToken},
		catalog.Submission{Exchange: readFixture(t, "book-starter-kit.txt")},
	)
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if updated.Revision.VersionNumber != 2 {
		t.Fatalf("version number = %d, want 2", updated.Revision.VersionNumber)
	}

	revisions, err := service.ListRevisions(ctx, entry.ID, "")
	if err != nil {
		t.Fatalf("list revisions: %v", err)
	}
	if len(revisions) != 2 {
		t.Fatalf("revision count = %d, want 2", len(revisions))
	}
}

func TestDeleteRevisionPromotesPrevious(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"),
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	auth := catalog.Authorization{EditToken: entry.EditToken}
	first := entry.Revision.ID

	second, err := service.CreateRevision(ctx, entry.ID, auth,
		catalog.Submission{Exchange: readFixture(t, "book-starter-kit.txt")})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}

	promoted, err := service.DeleteRevision(ctx, entry.ID, second.Revision.ID, auth)
	if err != nil {
		t.Fatalf("delete revision: %v", err)
	}
	if promoted == nil || promoted.ID != first {
		t.Fatalf("promoted revision = %v, want %s", promoted, first)
	}

	// The only remaining revision must not be removable.
	if _, err := service.DeleteRevision(ctx, entry.ID, first, auth); err == nil {
		t.Fatal("deleting the last revision should be rejected")
	}
}

// TestDeleteEntrySoftDeletes checks that deleting an entry stamps deleted_at
// rather than cascading a hard delete: revisions, nodes, and previews survive
// so entry_search_jobs can still resolve the entry when it later issues a
// Typesense removal, but the entry itself is no longer readable.
func TestDeleteEntrySoftDeletes(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "book-starter-kit.txt"),
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if err := service.DeleteEntry(ctx, entry.ID,
		catalog.Authorization{EditToken: entry.EditToken}); err != nil {
		t.Fatalf("delete entry: %v", err)
	}

	var remaining int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM blueprints").Scan(&remaining); err != nil {
		t.Fatalf("count blueprints: %v", err)
	}
	if remaining == 0 {
		t.Fatal("soft delete must not cascade-remove blueprint rows")
	}
	if _, err := service.GetEntry(ctx, entry.ID, ""); err == nil {
		t.Fatal("deleted entry should not be readable")
	}

	var deletedAt sql.NullTime
	if err := pool.QueryRow(ctx,
		"SELECT deleted_at FROM entries WHERE id = $1", entry.ID,
	).Scan(&deletedAt); err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatal("entries.deleted_at was not stamped")
	}

	var searchAction string
	if err := pool.QueryRow(ctx,
		"SELECT action FROM entry_search_jobs WHERE entry_id = $1", entry.ID,
	).Scan(&searchAction); err != nil {
		t.Fatalf("read search job: %v", err)
	}
	if searchAction != "delete" {
		t.Fatalf("search job action = %q, want %q", searchAction, "delete")
	}
}

func TestLeafPositionsFollowTreeOrder(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "book-starter-kit.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	leaves := entry.Revision.Root.Leaves()
	for want, leaf := range leaves {
		var got int32
		if err := pool.QueryRow(ctx, "SELECT position FROM blueprints WHERE id = $1", leaf.ID).Scan(&got); err != nil {
			t.Fatalf("read position: %v", err)
		}
		if int(got) != want {
			t.Fatalf("leaf %d (%s) position = %d, want %d", want, leaf.ID, got, want)
		}
	}
}
