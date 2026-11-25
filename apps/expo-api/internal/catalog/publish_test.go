package catalog_test

import (
	"context"
	"errors"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// An anonymous entry is claimed like a quick preview is published: the
// signed-in holder of its edit token becomes the owner, without waiting for
// its leaves.
func TestPublishAnonymousEntry(t *testing.T) {
	service, pool, user, other := newSocialFixtures(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Tags: []string{"mid-game"}})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if entry.EditToken == "" || entry.OwnerUserID != "" {
		t.Fatalf("anonymous entry = %+v, want an edit token and no owner", entry)
	}
	request := catalog.EntryPublish{Title: "Claimed", Description: "Mine now", Tags: &[]string{}, Visibility: "private"}
	claim := catalog.Authorization{UserID: user, EditToken: entry.EditToken}

	if _, err := service.PublishEntry(ctx, entry.ID, catalog.Authorization{EditToken: entry.EditToken}, nil, request); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("signed-out publish error = %v, want ErrForbidden", err)
	}
	if _, err := service.PublishEntry(ctx, entry.ID, catalog.Authorization{UserID: other, EditToken: "wrong"}, nil, request); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("wrong token publish error = %v, want ErrForbidden", err)
	}

	// Its preview is still pending: an anonymous entry is claimed as it is.
	published, err := service.PublishEntry(ctx, entry.ID, claim, nil, request)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if published.OwnerUserID != user || published.Visibility != "private" || published.ExpiresAt != nil ||
		published.Title != "Claimed" || published.Description != "Mine now" || len(published.Tags) != 0 ||
		published.EditToken != "" || published.Revision == nil || published.Revision.ID != entry.Revision.ID {
		t.Fatalf("published entry = %+v", published)
	}
	var token *string
	if err := pool.QueryRow(ctx, "SELECT edit_token_hash FROM entries WHERE id = $1", entry.ID).Scan(&token); err != nil || token != nil {
		t.Fatalf("edit token after publish = %v, %v; want cleared", token, err)
	}
	// The upload queued one index job; publishing queues a reindex (the job
	// row is per entry, so it stays one).
	var searchJobs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM entry_search_jobs WHERE entry_id = $1", entry.ID).Scan(&searchJobs); err != nil || searchJobs != 1 {
		t.Fatalf("search jobs after publish = %d, %v; want 1", searchJobs, err)
	}

	// The old token is dead, and an owned entry has nothing left to publish.
	if _, err := service.PublishEntry(ctx, entry.ID, claim, nil, request); !errors.Is(err, catalog.ErrAlreadyPublished) {
		t.Fatalf("second publish error = %v, want ErrAlreadyPublished", err)
	}
	if err := service.DeleteEntry(ctx, entry.ID, catalog.Authorization{EditToken: entry.EditToken}); !errors.Is(err, catalog.ErrNotFound) && !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("delete with the old token error = %v, want refused", err)
	}
	if mine, total, err := service.ListMyEntries(ctx, user, 20, 0); err != nil || total != 1 || len(mine) != 1 || mine[0].ID != entry.ID {
		t.Fatalf("my entries = %+v (total %d), %v; want the claimed entry", mine, total, err)
	}
}

// Publishing without tags (the form's tag list failed to load) keeps the
// entry's tags instead of clearing them.
func TestPublishWithoutTagsKeepsThem(t *testing.T) {
	service, _, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Tags: []string{"mid-game"}})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	published, err := service.PublishEntry(ctx, entry.ID, catalog.Authorization{UserID: user, EditToken: entry.EditToken}, nil,
		catalog.EntryPublish{Title: "Claimed", Visibility: "public"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(published.Tags) != 1 || published.Tags[0] != "mid-game" {
		t.Fatalf("tags after publishing without tags = %v, want [mid-game]", published.Tags)
	}
}
