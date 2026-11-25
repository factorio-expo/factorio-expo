package catalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// An upload with a lifetime is a regular entry until it expires: listed,
// indexed and editable; then it reads as missing and the sweep soft-deletes it
// with a search delete, leaving quick previews to their own sweep.
func TestExpiringUpload(t *testing.T) {
	service, pool, user, _ := newSocialFixtures(t)
	ctx := context.Background()

	if _, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), ExpiresInDays: 3}); !errors.Is(err, catalog.ErrInvalidExpiry) {
		t.Fatalf("3-day upload error = %v, want ErrInvalidExpiry", err)
	}
	never, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: user})
	if err != nil || never.ExpiresAt != nil {
		t.Fatalf("default upload = %+v, %v; want no expiry", never.ExpiresAt, err)
	}

	before := time.Now()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: user, ExpiresInDays: 7})
	if err != nil {
		t.Fatalf("create expiring entry: %v", err)
	}
	if entry.ExpiresAt == nil || entry.QuickPreview {
		t.Fatalf("expiring entry = %+v, want an expiry and no quick preview", entry)
	}
	if ttl := entry.ExpiresAt.Sub(before); ttl < 7*24*time.Hour-time.Minute || ttl > 7*24*time.Hour+time.Minute {
		t.Fatalf("entry expires in %s, want about 7 days", ttl)
	}

	// Listed (with its expiry), indexed, editable, collectable.
	mine, total, err := service.ListMyEntries(ctx, user, 20, 0)
	if err != nil || total != 2 {
		t.Fatalf("my entries = %+v (total %d), %v; want both", mine, total, err)
	}
	for _, card := range mine {
		if (card.ID == entry.ID) != (card.ExpiresAt != nil) {
			t.Fatalf("card %s expiry = %v", card.ID, card.ExpiresAt)
		}
	}
	assertSearchJob(t, pool, entry.ID, "index")
	title := "Still editable"
	if _, err := service.UpdateEntryMetadata(ctx, entry.ID, catalog.Authorization{UserID: user}, catalog.EntryMetadataUpdate{Title: &title}); err != nil {
		t.Fatalf("edit expiring entry: %v", err)
	}
	if err := service.AddFavorite(ctx, user, entry.ID); err != nil {
		t.Fatalf("favorite expiring entry: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatalf("drain search jobs: %v", err)
	}

	// Expired: missing at once; the preview sweep leaves it, the entry sweep
	// soft-deletes it and queues the search delete.
	if _, err := pool.Exec(ctx, "UPDATE entries SET expires_at = now() - interval '1 second' WHERE id = $1", entry.ID); err != nil {
		t.Fatalf("expire entry: %v", err)
	}
	if _, err := service.GetEntry(ctx, entry.ID, user); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("expired read error = %v, want ErrNotFound", err)
	}
	if _, total, _ := service.ListMyEntries(ctx, user, 20, 0); total != 1 {
		t.Fatalf("my entries after expiry = %d, want 1", total)
	}
	for range 2 {
		// The second sweep finds nothing left to do.
		if _, err := service.SweepExpiredEntries(ctx); err != nil {
			t.Fatalf("sweep: %v", err)
		}
	}
	assertEntryRows(t, pool, entry.ID, 1)
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT deleted_at FROM entries WHERE id = $1", entry.ID).Scan(&deletedAt); err != nil || deletedAt == nil {
		t.Fatalf("deleted_at = %v, %v; want set", deletedAt, err)
	}
	assertSearchJob(t, pool, entry.ID, "delete")
}

// Publishing sets the expiry the caller chose; absent, an anonymous entry
// keeps its own and a quick preview gets none.
func TestPublishExpiry(t *testing.T) {
	service, pool, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	days := func(n int) *int { return &n }
	publish := func(entry catalog.Entry, expiresInDays *int) catalog.Entry {
		t.Helper()
		published, err := service.PublishEntry(ctx, entry.ID, catalog.Authorization{UserID: user, EditToken: entry.EditToken}, nil,
			catalog.EntryPublish{Title: "Mine", Visibility: "public", ExpiresInDays: expiresInDays})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		if published.QuickPreview {
			t.Fatalf("published entry is still a quick preview")
		}
		return published
	}
	upload := func(expiresInDays int) catalog.Entry {
		t.Helper()
		entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), ExpiresInDays: expiresInDays})
		if err != nil {
			t.Fatalf("upload: %v", err)
		}
		return entry
	}

	kept := upload(14)
	if got := publish(kept, nil); got.ExpiresAt == nil || !got.ExpiresAt.Equal(*kept.ExpiresAt) {
		t.Fatalf("kept expiry = %v, want %v", got.ExpiresAt, kept.ExpiresAt)
	}
	if got := publish(upload(14), days(0)); got.ExpiresAt != nil {
		t.Fatalf("cleared expiry = %v, want none", got.ExpiresAt)
	}
	if got := publish(upload(0), days(1)); got.ExpiresAt == nil || time.Until(*got.ExpiresAt) > 25*time.Hour {
		t.Fatalf("1-day expiry = %v", got.ExpiresAt)
	}
	entry := upload(0)
	if _, err := service.PublishEntry(ctx, entry.ID, catalog.Authorization{UserID: user, EditToken: entry.EditToken}, nil,
		catalog.EntryPublish{Title: "Mine", Visibility: "public", ExpiresInDays: days(2)}); !errors.Is(err, catalog.ErrInvalidExpiry) {
		t.Fatalf("2-day publish error = %v, want ErrInvalidExpiry", err)
	}

	for _, tc := range []struct {
		days *int
		want bool
	}{{nil, false}, {days(30), true}} {
		preview, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", nil)
		if err != nil {
			t.Fatalf("create preview: %v", err)
		}
		finishPreview(t, pool, preview)
		got := publish(preview, tc.days)
		if (got.ExpiresAt != nil) != tc.want || (tc.want && time.Until(*got.ExpiresAt) < 29*24*time.Hour) {
			t.Fatalf("published preview expiry = %v, want set %v", got.ExpiresAt, tc.want)
		}
	}
}

func assertSearchJob(t *testing.T, pool *pgxpool.Pool, entryID, action string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(context.Background(), "SELECT action FROM entry_search_jobs WHERE entry_id = $1", entryID).Scan(&got); err != nil || got != action {
		t.Fatalf("search job for %s = %q, %v; want %q", entryID, got, err, action)
	}
}
