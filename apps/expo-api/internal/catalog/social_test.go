package catalog_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// newSocialFixtures builds a catalog.Service with a clean slate for
// favorites/collections tests: it truncates entries (via newService) plus
// users and collections (which cascade to favorites and collection_entries),
// then inserts two fixed test users. Tags are deliberately left untouched:
// they are curated, migration-seeded rows, not per-test fixtures.
func newSocialFixtures(t *testing.T) (service *catalog.Service, pool *pgxpool.Pool, userA, userB string) {
	t.Helper()
	service, pool = newService(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "TRUNCATE users, collections CASCADE"); err != nil {
		t.Fatalf("truncate users: %v", err)
	}

	const insertUser = `
		INSERT INTO users (id, username) VALUES ($1, $2)`
	userA = "socialUserA1"
	userB = "socialUserB1"
	if _, err := pool.Exec(ctx, insertUser, userA, "social-test-alice"); err != nil {
		t.Fatalf("insert user A: %v", err)
	}
	if _, err := pool.Exec(ctx, insertUser, userB, "social-test-bob"); err != nil {
		t.Fatalf("insert user B: %v", err)
	}
	return service, pool, userA, userB
}

func TestFavoritesAreIdempotentAndScopedToOwner(t *testing.T) {
	service, _, userA, _ := newSocialFixtures(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	if err := service.AddFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	// Favoriting twice must not error or duplicate the row.
	if err := service.AddFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatalf("add favorite again: %v", err)
	}

	favorites, _, err := service.ListFavorites(ctx, userA, 10, 0)
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	if len(favorites) != 1 || favorites[0].ID != entry.ID {
		t.Fatalf("favorites = %+v, want exactly one entry %s", favorites, entry.ID)
	}

	if err := service.RemoveFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatalf("remove favorite: %v", err)
	}
	// Removing an already-removed favorite must not error.
	if err := service.RemoveFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatalf("remove favorite again: %v", err)
	}

	favorites, _, err = service.ListFavorites(ctx, userA, 10, 0)
	if err != nil {
		t.Fatalf("list favorites after removal: %v", err)
	}
	if len(favorites) != 0 {
		t.Fatalf("favorites after removal = %+v, want none", favorites)
	}
}

func TestAddFavoriteRejectsUnknownEntry(t *testing.T) {
	service, _, userA, _ := newSocialFixtures(t)
	ctx := context.Background()

	err := service.AddFavorite(ctx, userA, "000000000000")
	if err == nil {
		t.Fatal("favoriting a nonexistent entry should be rejected")
	}
}

// Every read that shows an entry to a viewer says whether that viewer
// favorited it, so pages can render the state without a separate lookup.
func TestReadsCarryTheViewersFavoritedState(t *testing.T) {
	service, _, userA, userB := newSocialFixtures(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: userB})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if err := service.AddFavorite(ctx, userA, entry.ID); err != nil {
		t.Fatalf("add favorite: %v", err)
	}
	collection, err := service.CreateCollection(ctx, userB, "Shared", "", "public")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if err := service.AddCollectionEntry(ctx, collection.ID, userB, entry.ID, 0); err != nil {
		t.Fatalf("add collection entry: %v", err)
	}

	for _, viewer := range []struct {
		name, id string
		want     bool
	}{{"favoriting viewer", userA, true}, {"other viewer", userB, false}, {"anonymous", "", false}} {
		t.Run(viewer.name, func(t *testing.T) {
			read, err := service.GetEntry(ctx, entry.ID, viewer.id)
			if err != nil {
				t.Fatalf("get entry: %v", err)
			}
			if read.IsFavorited != viewer.want {
				t.Errorf("GetEntry IsFavorited = %v, want %v", read.IsFavorited, viewer.want)
			}

			listed, _, err := service.ListUserEntries(ctx, "social-test-bob", viewer.id, 10, 0)
			if err != nil {
				t.Fatalf("list user entries: %v", err)
			}
			if len(listed) != 1 || listed[0].IsFavorited != viewer.want {
				t.Errorf("ListUserEntries = %+v, want one entry with IsFavorited %v", listed, viewer.want)
			}

			hydrated, err := service.GetCollection(ctx, collection.ID, viewer.id)
			if err != nil {
				t.Fatalf("get collection: %v", err)
			}
			if len(hydrated.Entries) != 1 || hydrated.Entries[0].IsFavorited != viewer.want {
				t.Errorf("GetCollection entries = %+v, want one entry with IsFavorited %v", hydrated.Entries, viewer.want)
			}

			if viewer.id == "" {
				return
			}
			favorited, err := service.IsFavorite(ctx, viewer.id, entry.ID)
			if err != nil {
				t.Fatalf("is favorite: %v", err)
			}
			if favorited != viewer.want {
				t.Errorf("IsFavorite = %v, want %v", favorited, viewer.want)
			}
		})
	}

	favorites, _, err := service.ListFavorites(ctx, userA, 10, 0)
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	if len(favorites) != 1 || !favorites[0].IsFavorited {
		t.Fatalf("ListFavorites = %+v, want one entry with IsFavorited", favorites)
	}
}

// The owner's own listing is the way back to a private or unlisted entry: it
// has all of them, newest first, and nobody else's; the public profile keeps
// showing public entries only.
func TestListMyEntriesIncludesPrivateEntries(t *testing.T) {
	service, _, userA, userB := newSocialFixtures(t)
	ctx := context.Background()

	create := func(owner, visibility string) catalog.Entry {
		t.Helper()
		entry, err := service.CreateEntry(ctx, catalog.Submission{
			Exchange: readFixture(t, "bp-starter-smelter.txt"), OwnerUserID: owner, Visibility: visibility,
		})
		if err != nil {
			t.Fatalf("create %s entry: %v", visibility, err)
		}
		return entry
	}
	public := create(userA, "public")
	unlisted := create(userA, "unlisted")
	private := create(userA, "private")
	create(userB, "public")
	create("", "public")

	mine, total, err := service.ListMyEntries(ctx, userA, 10, 0)
	if err != nil {
		t.Fatalf("list my entries: %v", err)
	}
	if total != 3 || len(mine) != 3 || mine[0].ID != private.ID || mine[1].ID != unlisted.ID || mine[2].ID != public.ID {
		t.Fatalf("ListMyEntries = %+v (total %d), want [private, unlisted, public] of 3", mine, total)
	}
	if mine[0].Visibility != "private" || mine[1].Visibility != "unlisted" || mine[2].Visibility != "public" {
		t.Errorf("visibilities = %q, %q, %q, want private, unlisted, public",
			mine[0].Visibility, mine[1].Visibility, mine[2].Visibility)
	}

	page, total, err := service.ListMyEntries(ctx, userA, 1, 2)
	if err != nil {
		t.Fatalf("list my entries page 3: %v", err)
	}
	if total != 3 || len(page) != 1 || page[0].ID != public.ID {
		t.Errorf("third page = %+v (total %d), want [public] of 3", page, total)
	}

	for name, viewer := range map[string]string{"owner": userA, "other user": userB, "anonymous": ""} {
		profile, total, err := service.ListUserEntries(ctx, "social-test-alice", viewer, 10, 0)
		if err != nil {
			t.Fatalf("list user entries: %v", err)
		}
		if total != 1 || len(profile) != 1 || profile[0].ID != public.ID {
			t.Errorf("public profile seen by %s = %+v (total %d), want only the public entry", name, profile, total)
		}
	}
}

func TestCollectionLifecycleAndOwnership(t *testing.T) {
	service, _, userA, userB := newSocialFixtures(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	collection, err := service.CreateCollection(ctx, userA, "Factory Ideas", "a description", "private")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if collection.Visibility != "private" {
		t.Fatalf("visibility = %q, want private", collection.Visibility)
	}

	// A private collection is invisible to another user...
	if _, err := service.GetCollection(ctx, collection.ID, userB); err == nil {
		t.Fatal("another user should not be able to read a private collection")
	}
	// ...and to an anonymous caller.
	if _, err := service.GetCollection(ctx, collection.ID, ""); err == nil {
		t.Fatal("an anonymous caller should not be able to read a private collection")
	}
	// The owner can always read it.
	if _, err := service.GetCollection(ctx, collection.ID, userA); err != nil {
		t.Fatalf("owner get collection: %v", err)
	}

	// Only the owner may add entries.
	if err := service.AddCollectionEntry(ctx, collection.ID, userB, entry.ID, 0); err == nil {
		t.Fatal("a non-owner should not be able to add entries to a collection")
	}
	if err := service.AddCollectionEntry(ctx, collection.ID, userA, entry.ID, 0); err != nil {
		t.Fatalf("add collection entry: %v", err)
	}

	hydrated, err := service.GetCollection(ctx, collection.ID, userA)
	if err != nil {
		t.Fatalf("get collection: %v", err)
	}
	if len(hydrated.Entries) != 1 || hydrated.Entries[0].ID != entry.ID {
		t.Fatalf("collection entries = %+v, want exactly entry %s", hydrated.Entries, entry.ID)
	}

	// Flipping visibility to public makes it readable by anyone.
	updated, err := service.UpdateCollection(ctx, collection.ID, userA, "Factory Ideas", "updated", "public")
	if err != nil {
		t.Fatalf("update collection: %v", err)
	}
	if updated.Visibility != "public" || updated.Description != "updated" {
		t.Fatalf("updated collection = %+v", updated)
	}
	if _, err := service.GetCollection(ctx, collection.ID, userB); err != nil {
		t.Fatalf("public collection should be readable by anyone: %v", err)
	}

	// Only the owner may remove entries or delete the collection.
	if err := service.RemoveCollectionEntry(ctx, collection.ID, userB, entry.ID); err == nil {
		t.Fatal("a non-owner should not be able to remove entries from a collection")
	}
	if err := service.DeleteCollection(ctx, collection.ID, userB); err == nil {
		t.Fatal("a non-owner should not be able to delete a collection")
	}
	if err := service.RemoveCollectionEntry(ctx, collection.ID, userA, entry.ID); err != nil {
		t.Fatalf("remove collection entry: %v", err)
	}
	if err := service.DeleteCollection(ctx, collection.ID, userA); err != nil {
		t.Fatalf("delete collection: %v", err)
	}
	if _, err := service.GetCollection(ctx, collection.ID, userA); err == nil {
		t.Fatal("deleted collection should not be readable")
	}
}

func TestCreateCollectionValidatesFields(t *testing.T) {
	service, _, userA, _ := newSocialFixtures(t)
	ctx := context.Background()

	if _, err := service.CreateCollection(ctx, userA, "", "", ""); err == nil {
		t.Fatal("an empty name should be rejected")
	}
	if _, err := service.CreateCollection(ctx, userA, "Name", "", "not-a-visibility"); err == nil {
		t.Fatal("an invalid visibility should be rejected")
	}

	collection, err := service.CreateCollection(ctx, userA, "Name", "", "")
	if err != nil {
		t.Fatalf("create collection with default visibility: %v", err)
	}
	if collection.Visibility != "private" {
		t.Fatalf("default visibility = %q, want private", collection.Visibility)
	}
}

// TestListTagGroupsReturnsCuratedSet checks that the migration-seeded, fixed
// tag taxonomy is readable, grouped and ordered; tags are never created
// through the API.
func TestListTagGroupsReturnsCuratedSet(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()

	groups, err := service.ListTagGroups(ctx)
	if err != nil {
		t.Fatalf("list tag groups: %v", err)
	}
	if len(groups) == 0 {
		t.Fatal("expected the curated tag set to be non-empty")
	}
	if groups[0].Slug != "progression" || groups[0].Tags[0].Slug != "bootstrap" {
		t.Fatalf("first group/tag = %q/%q, want progression/bootstrap", groups[0].Slug, groups[0].Tags[0].Slug)
	}

	slugs := map[string]bool{}
	for _, group := range groups {
		if len(group.Tags) == 0 {
			t.Fatalf("group %q has no tags", group.Slug)
		}
		for _, tag := range group.Tags {
			if slugs[tag.Slug] {
				t.Fatalf("tag %q listed twice", tag.Slug)
			}
			slugs[tag.Slug] = true
			if tag.Slug == "starter-base" && (group.Slug != "base-style" || tag.DisplayName != "Starter base") {
				t.Fatalf("starter-base = %q in %q, want Starter base in base-style", tag.DisplayName, group.Slug)
			}
		}
	}
	if !slugs["starter-base"] {
		t.Fatal("expected the curated set to include starter-base")
	}
}

// Every listing builds its cards through the one card query: the listing's own
// order survives, and each card carries the favorites count and cover state.
func TestListingCardsKeepTheirOrderAndCounts(t *testing.T) {
	service, _, userA, userB := newSocialFixtures(t)
	ctx := context.Background()

	firstTitle, secondTitle := "First", "Second"
	first, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Title: &firstTitle})
	if err != nil {
		t.Fatalf("create first entry: %v", err)
	}
	second, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt"), Title: &secondTitle})
	if err != nil {
		t.Fatalf("create second entry: %v", err)
	}
	for _, user := range []string{userA, userB} {
		if err := service.AddFavorite(ctx, user, first.ID); err != nil {
			t.Fatalf("add favorite: %v", err)
		}
	}
	collection, err := service.CreateCollection(ctx, userA, "Ordered", "", "private")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	// Stored positions put the newer entry first.
	if err := service.AddCollectionEntry(ctx, collection.ID, userA, first.ID, 2); err != nil {
		t.Fatalf("add first: %v", err)
	}
	if err := service.AddCollectionEntry(ctx, collection.ID, userA, second.ID, 1); err != nil {
		t.Fatalf("add second: %v", err)
	}

	hydrated, err := service.GetCollection(ctx, collection.ID, userA)
	if err != nil {
		t.Fatalf("get collection: %v", err)
	}
	if len(hydrated.Entries) != 2 || hydrated.Entries[0].ID != second.ID || hydrated.Entries[1].ID != first.ID {
		t.Fatalf("collection order = %+v, want second then first", hydrated.Entries)
	}
	if got := hydrated.Entries[1]; got.FavoritesCount != 2 || !got.IsFavorited || got.Title != "First" || got.LeafCount != 1 {
		t.Fatalf("first card = %+v, want 2 favorites, favorited by the viewer, one leaf", got)
	}
	if got := hydrated.Entries[0]; got.FavoritesCount != 0 || got.IsFavorited {
		t.Fatalf("second card = %+v, want no favorites", got)
	}

	cards, err := service.EntryCards(ctx, []string{second.ID, first.ID}, "")
	if err != nil {
		t.Fatalf("entry cards: %v", err)
	}
	if len(cards) != 2 || cards[first.ID].FavoritesCount != 2 || cards[first.ID].IsFavorited {
		t.Fatalf("anonymous entry cards = %+v, want both, first with 2 favorites, none favorited", cards)
	}
}

// An expired favorite takes no slot on a favorites page: the id query and the
// card query apply the same live rule, so a full page is never cut short.
func TestExpiredFavoriteTakesNoPageSlot(t *testing.T) {
	service, pool, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	kept, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create kept entry: %v", err)
	}
	expired, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create expiring entry: %v", err)
	}
	// The expired one is the newest favorite, so it would head the first page.
	for _, id := range []string{kept.ID, expired.ID} {
		if err := service.AddFavorite(ctx, user, id); err != nil {
			t.Fatalf("favorite %s: %v", id, err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE entries SET expires_at = now() - interval '1 minute' WHERE id = $1", expired.ID); err != nil {
		t.Fatalf("expire entry: %v", err)
	}
	page, _, err := service.ListFavorites(ctx, user, 1, 0)
	if err != nil {
		t.Fatalf("list favorites: %v", err)
	}
	if len(page) != 1 || page[0].ID != kept.ID {
		t.Fatalf("first page = %+v, want the live favorite", page)
	}
}

// A card without a cover says whether a preview is still on its way: pending
// until a leaf renders (then the cover takes over) or its preview job fails.
func TestCardPreviewPending(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	card := func(id string) catalog.EntrySummary {
		t.Helper()
		cards, err := service.EntryCards(ctx, []string{id}, "")
		if err != nil {
			t.Fatalf("entry cards: %v", err)
		}
		return cards[id]
	}

	rendered, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if got := card(rendered.ID); !got.PreviewPending || got.PreviewURL != "" {
		t.Fatalf("new entry card = %+v, want a pending preview and no cover", got)
	}
	sha := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	if err := service.StorePreview(ctx, rendered.Revision.Root.Leaves()[0].ID, sha, 64, 64); err != nil {
		t.Fatalf("store preview: %v", err)
	}
	if got := card(rendered.ID); got.PreviewPending || got.PreviewURL == "" {
		t.Fatalf("rendered entry card = %+v, want a cover and nothing pending", got)
	}

	failed, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE phase_jobs SET state = 'failed' WHERE phase = 'preview' AND blueprint_id = $1",
		failed.Revision.Root.Leaves()[0].ID); err != nil {
		t.Fatalf("fail preview job: %v", err)
	}
	if got := card(failed.ID); got.PreviewPending || got.PreviewURL != "" {
		t.Fatalf("failed entry card = %+v, want neither a cover nor a pending preview", got)
	}
}
