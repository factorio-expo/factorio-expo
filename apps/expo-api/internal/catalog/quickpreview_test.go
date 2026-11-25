package catalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

func TestQuickPreviewIsUnlistedAndReadOnly(t *testing.T) {
	service, pool, user, _ := newSocialFixtures(t)
	ctx := context.Background()

	if _, err := service.CreateQuickPreview(ctx, readFixture(t, "book-starter-kit.txt"), "", nil); !errors.Is(err, catalog.ErrPreviewOfBook) {
		t.Fatalf("book preview error = %v, want ErrPreviewOfBook", err)
	}

	before := time.Now()
	preview, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if preview.EditToken == "" || preview.Visibility != "public" || preview.ExpiresAt == nil {
		t.Fatalf("anonymous preview = %+v, want public, an edit token and an expiry", preview)
	}
	if ttl := preview.ExpiresAt.Sub(before); ttl < catalog.DefaultPreviewTTL-time.Minute || ttl > catalog.DefaultPreviewTTL+time.Minute {
		t.Fatalf("preview expires in %s, want about %s", ttl, catalog.DefaultPreviewTTL)
	}
	if preview.Revision == nil || preview.Revision.Root == nil || preview.Revision.Root.Kind != "blueprint" {
		t.Fatalf("preview revision = %+v", preview.Revision)
	}
	if got, err := service.GetEntry(ctx, preview.ID, ""); err != nil || got.ExpiresAt == nil {
		t.Fatalf("read preview = %+v, %v", got, err)
	}

	// Never indexed, never listed.
	var searchJobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM entry_search_jobs WHERE entry_id = $1", preview.ID).Scan(&searchJobs); err != nil {
		t.Fatalf("count search jobs: %v", err)
	}
	if searchJobs != 0 {
		t.Fatalf("preview queued %d search jobs, want none", searchJobs)
	}
	if ids, err := db.New(pool).ListSearchableEntryIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("searchable ids = %v, %v; want none", ids, err)
	}
	owned, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), user, nil)
	if err != nil {
		t.Fatalf("create owned preview: %v", err)
	}
	if owned.EditToken != "" || owned.OwnerUserID != user {
		t.Fatalf("signed-in preview = %+v, want owned without a token", owned)
	}
	if mine, total, err := service.ListMyEntries(ctx, user, 20, 0); err != nil || total != 0 || len(mine) != 0 {
		t.Fatalf("my entries = %+v (total %d), %v; want previews left out", mine, total, err)
	}

	// Read-only until published: edits, revisions, deletes, favorites, collections.
	auth := catalog.Authorization{EditToken: preview.EditToken}
	title := "Renamed"
	if _, err := service.UpdateEntryMetadata(ctx, preview.ID, auth, catalog.EntryMetadataUpdate{Title: &title}); !errors.Is(err, catalog.ErrQuickPreview) {
		t.Fatalf("metadata edit error = %v, want ErrQuickPreview", err)
	}
	if _, err := service.CreateRevision(ctx, preview.ID, auth, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")}); !errors.Is(err, catalog.ErrQuickPreview) {
		t.Fatalf("revision error = %v, want ErrQuickPreview", err)
	}
	if err := service.DeleteEntry(ctx, preview.ID, auth); !errors.Is(err, catalog.ErrQuickPreview) {
		t.Fatalf("delete error = %v, want ErrQuickPreview", err)
	}
	if err := service.AddFavorite(ctx, user, preview.ID); !errors.Is(err, catalog.ErrQuickPreview) {
		t.Fatalf("favorite error = %v, want ErrQuickPreview", err)
	}
	if can, err := service.CanEditEntry(ctx, preview.ID, auth); err != nil || !can {
		t.Fatalf("token holder can edit = %v, %v; want true (may publish)", can, err)
	}
}

func TestPublishEntry(t *testing.T) {
	service, pool, user, other := newSocialFixtures(t)
	ctx := context.Background()
	preview, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	request := catalog.EntryPublish{Title: "  Smelter  ", Description: "Notes", Tags: &[]string{"mid-game"}, Visibility: "private"}
	claim := catalog.Authorization{UserID: user, EditToken: preview.EditToken}

	if _, err := service.PublishEntry(ctx, preview.ID, catalog.Authorization{EditToken: preview.EditToken}, nil, request); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("anonymous publish error = %v, want ErrForbidden", err)
	}
	if _, err := service.PublishEntry(ctx, preview.ID, catalog.Authorization{UserID: other}, nil, request); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("publish without the token error = %v, want ErrForbidden", err)
	}
	if _, err := service.PublishEntry(ctx, preview.ID, claim, nil, request); !errors.Is(err, catalog.ErrPreviewNotReady) {
		t.Fatalf("unfinished publish error = %v, want ErrPreviewNotReady", err)
	}
	if _, err := service.PublishEntry(ctx, preview.ID, claim, nil, catalog.EntryPublish{Title: " "}); !errors.Is(err, catalog.ErrInvalidEntryMetadata) {
		t.Fatalf("blank title error = %v, want ErrInvalidEntryMetadata", err)
	}

	finishPreview(t, pool, preview)
	published, err := service.PublishEntry(ctx, preview.ID, claim, nil, request)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.ExpiresAt != nil || published.OwnerUserID != user || published.Visibility != "private" ||
		published.Title != "Smelter" || published.Description != "Notes" ||
		len(published.Tags) != 1 || published.Tags[0] != "mid-game" ||
		published.Revision == nil || published.Revision.ID != preview.Revision.ID {
		t.Fatalf("published entry = %+v", published)
	}
	var token *string
	if err := pool.QueryRow(ctx, "SELECT edit_token_hash FROM entries WHERE id = $1", preview.ID).Scan(&token); err != nil || token != nil {
		t.Fatalf("edit token after publish = %v, %v; want cleared", token, err)
	}
	var searchJobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM entry_search_jobs WHERE entry_id = $1", preview.ID).Scan(&searchJobs); err != nil || searchJobs != 1 {
		t.Fatalf("search jobs after publish = %d, %v; want 1", searchJobs, err)
	}
	if _, err := service.PublishEntry(ctx, preview.ID, claim, nil, request); !errors.Is(err, catalog.ErrAlreadyPublished) {
		t.Fatalf("second publish error = %v, want ErrAlreadyPublished", err)
	}
	// A regular entry now: editable by its owner.
	title := "Smelter v2"
	if _, err := service.UpdateEntryMetadata(ctx, preview.ID, catalog.Authorization{UserID: user}, catalog.EntryMetadataUpdate{Title: &title}); err != nil {
		t.Fatalf("edit published entry: %v", err)
	}
}

func TestExpiredAndRejectedPreviewsAreGone(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	regular, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	failed, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	expired, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}

	// A failed job rejects a preview, never a regular entry.
	for _, entry := range []catalog.Entry{regular, failed} {
		if err := service.RejectQuickPreview(ctx, entry.ID); err != nil {
			t.Fatalf("reject %s: %v", entry.ID, err)
		}
	}
	assertEntryRows(t, pool, regular.ID, 1)
	assertEntryRows(t, pool, failed.ID, 0)

	// Past its expiry a preview reads as missing at once, then the sweep removes it.
	if _, err := pool.Exec(ctx, "UPDATE entries SET expires_at = now() - interval '1 second' WHERE id = $1", expired.ID); err != nil {
		t.Fatalf("expire preview: %v", err)
	}
	if _, err := service.GetEntry(ctx, expired.ID, ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expired read error = %v, want ErrNotFound", err)
	}
	if _, err := service.SweepExpiredEntries(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	assertEntryRows(t, pool, expired.ID, 0)
	assertEntryRows(t, pool, regular.ID, 1)
}

// finishPreview stores a preview and a production snapshot for every leaf and
// drops their intents, as the dispatcher's import does.
func finishPreview(t *testing.T, pool *pgxpool.Pool, entry catalog.Entry) {
	t.Helper()
	ctx := context.Background()
	for _, leaf := range entry.Revision.Root.Leaves() {
		if _, err := pool.Exec(ctx, `
			INSERT INTO previews (blueprint_id, source_sha256, width, height)
			VALUES ($1, repeat('a', 64), 1, 1)`, leaf.ID); err != nil {
			t.Fatalf("insert preview: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			UPDATE blueprint_stats SET production_schema_version = 1, production_state = 'complete'
			WHERE blueprint_id = $1`, leaf.ID); err != nil {
			t.Fatalf("store production: %v", err)
		}
		if _, err := pool.Exec(ctx, `
			DELETE FROM phase_jobs WHERE blueprint_id = $1`, leaf.ID); err != nil {
			t.Fatalf("drop intents: %v", err)
		}
	}
}

// assertEntryRows checks how many rows the entry and its revisions have left.
func assertEntryRows(t *testing.T, pool *pgxpool.Pool, entryID string, want int) {
	t.Helper()
	var entries, revisions int
	if err := pool.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM entries WHERE id = $1),
		       (SELECT count(*) FROM revisions WHERE entry_id = $1)`, entryID,
	).Scan(&entries, &revisions); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if entries != want || revisions != want {
		t.Fatalf("entry %s has %d entry and %d revision rows, want %d each", entryID, entries, revisions, want)
	}
}
