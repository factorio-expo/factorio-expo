package catalog_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func pinnedIDs(t *testing.T, service *catalog.Service, username string) []string {
	t.Helper()
	cards, err := service.ListPinnedEntries(context.Background(), username, "")
	if err != nil {
		t.Fatalf("list pinned entries: %v", err)
	}
	ids := make([]string, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card.ID)
	}
	return ids
}

func TestPinsKeepOrderLimitAndOwnPublicEntries(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	exchange := readFixture(t, "bp-starter-smelter.txt")

	var own []string
	for range catalog.MaxPinnedEntries + 1 {
		entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userA, Visibility: "public"})
		if err != nil {
			t.Fatalf("create entry: %v", err)
		}
		own = append(own, entry.ID)
	}
	others, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userB, Visibility: "public"})
	if err != nil {
		t.Fatalf("create other entry: %v", err)
	}
	private, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userA, Visibility: "private"})
	if err != nil {
		t.Fatalf("create private entry: %v", err)
	}

	if err := service.PinEntry(ctx, userA, own[0]); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if err := service.PinEntry(ctx, userA, own[1]); err != nil {
		t.Fatalf("pin: %v", err)
	}
	// Pinning again changes nothing.
	if err := service.PinEntry(ctx, userA, own[0]); err != nil {
		t.Fatalf("pin again: %v", err)
	}
	if got := pinnedIDs(t, service, "social-test-alice"); !slices.Equal(got, own[:2]) {
		t.Fatalf("pins = %v, want %v", got, own[:2])
	}

	for _, id := range []string{others.ID, private.ID} {
		if err := service.PinEntry(ctx, userA, id); !errors.Is(err, catalog.ErrNotPinnable) {
			t.Fatalf("pin %s: %v, want ErrNotPinnable", id, err)
		}
	}

	reordered := []string{own[1], own[0], own[2]}
	if err := service.SetPins(ctx, userA, reordered); err != nil {
		t.Fatalf("set pins: %v", err)
	}
	if got := pinnedIDs(t, service, "social-test-alice"); !slices.Equal(got, reordered) {
		t.Fatalf("pins after reorder = %v, want %v", got, reordered)
	}
	if err := service.SetPins(ctx, userA, own); !errors.Is(err, catalog.ErrPinLimit) {
		t.Fatalf("set %d pins: %v, want ErrPinLimit", len(own), err)
	}
	if err := service.SetPins(ctx, userA, own[:catalog.MaxPinnedEntries]); err != nil {
		t.Fatalf("set the most pins: %v", err)
	}
	if err := service.PinEntry(ctx, userA, own[catalog.MaxPinnedEntries]); !errors.Is(err, catalog.ErrPinLimit) {
		t.Fatalf("pin past the limit: %v, want ErrPinLimit", err)
	}

	// An entry that stops being public drops out without a write, and no
	// longer counts towards the limit.
	if _, err := pool.Exec(ctx, "UPDATE entries SET visibility = 'unlisted' WHERE id = $1", own[0]); err != nil {
		t.Fatalf("make unlisted: %v", err)
	}
	want := slices.Clone(own[1:catalog.MaxPinnedEntries])
	if got := pinnedIDs(t, service, "social-test-alice"); !slices.Equal(got, want) {
		t.Fatalf("pins after unlisting = %v, want %v", got, want)
	}
	if err := service.PinEntry(ctx, userA, own[catalog.MaxPinnedEntries]); err != nil {
		t.Fatalf("pin into the freed slot: %v", err)
	}

	if err := service.UnpinEntry(ctx, userA, own[1]); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	cards, _, err := service.ListMyEntries(ctx, userA, 20, 0)
	if err != nil {
		t.Fatalf("list my entries: %v", err)
	}
	for _, card := range cards {
		pinned := slices.Contains(pinnedIDs(t, service, "social-test-alice"), card.ID)
		if card.IsPinned != pinned {
			t.Fatalf("card %s is_pinned = %v, want %v", card.ID, card.IsPinned, pinned)
		}
	}
}

func TestProfileStatusLinksAndStats(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()

	status := "Building on [planet=gleba]"
	links := []catalog.SocialLinkInput{{Platform: "github", Handle: "@someone"}}
	profile, err := service.UpdateProfile(ctx, userA, catalog.ProfileUpdate{Status: &status, Links: &links})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if profile.Status != status || len(profile.Links) != 1 || profile.Links[0].URL != "https://github.com/someone" {
		t.Fatalf("profile = %+v", profile)
	}
	// A missing part stays as it is.
	if profile, err = service.UpdateProfile(ctx, userA, catalog.ProfileUpdate{}); err != nil {
		t.Fatalf("empty update: %v", err)
	}
	if profile.Status != status || len(profile.Links) != 1 {
		t.Fatalf("profile after an empty update = %+v", profile)
	}

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: userA, Visibility: "public", Tags: []string{"early-game"},
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if err := service.AddFavorite(ctx, userB, entry.ID); err != nil {
		t.Fatalf("favorite: %v", err)
	}
	// One's own favorite does not count.
	if err := service.AddFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatalf("own favorite: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO entry_view_counts (entry_id, views) VALUES ($1, 41)", entry.ID); err != nil {
		t.Fatalf("views: %v", err)
	}
	profile, err = service.GetPublicUser(ctx, "SOCIAL-TEST-ALICE")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	stats := profile.Stats
	if stats.Entries != 1 || stats.Views != 41 || stats.Favorites != 1 {
		t.Fatalf("stats = %+v, want 1 entry, 41 views, 1 favorite", stats)
	}
	if len(stats.TopTags) != 1 || stats.TopTags[0].Slug != "early-game" || stats.TopTags[0].Entries != 1 {
		t.Fatalf("top tags = %+v", stats.TopTags)
	}
}

func TestCollectionListingsCountWhatTheViewerSees(t *testing.T) {
	service, pool, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	exchange := readFixture(t, "bp-starter-smelter.txt")

	collection, err := service.CreateCollection(ctx, userA, "Keepers", "", "public")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	shown, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userB, Visibility: "public"})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	hidden, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userA, Visibility: "public"})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	for _, id := range []string{shown.ID, hidden.ID} {
		if err := service.AddCollectionEntry(ctx, collection.ID, userA, id, 0); err != nil {
			t.Fatalf("add to collection: %v", err)
		}
	}
	// Only its owner sees a private entry.
	if _, err := pool.Exec(ctx, "UPDATE entries SET visibility = 'private' WHERE id = $1", hidden.ID); err != nil {
		t.Fatalf("make private: %v", err)
	}

	public, err := service.ListUserPublicCollections(ctx, "social-test-alice")
	if err != nil {
		t.Fatalf("list public collections: %v", err)
	}
	if len(public) != 1 || public[0].EntryCount != 1 {
		t.Fatalf("public collections = %+v, want one with 1 entry", public)
	}
	mine, err := service.ListCollections(ctx, userA)
	if err != nil {
		t.Fatalf("list my collections: %v", err)
	}
	if len(mine) != 1 || mine[0].EntryCount != 2 {
		t.Fatalf("my collections = %+v, want one with 2 entries", mine)
	}
}
