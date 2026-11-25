package catalog_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

func TestReorderCollectionEntriesKeepsMembersAndOwnerOnly(t *testing.T) {
	service, _, userA, userB := newSocialFixtures(t)
	ctx := context.Background()
	exchange := readFixture(t, "bp-starter-smelter.txt")

	var ids []string
	for range 3 {
		entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userA, Visibility: "public"})
		if err != nil {
			t.Fatalf("create entry: %v", err)
		}
		ids = append(ids, entry.ID)
	}
	outsider, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: userA, Visibility: "public"})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	collection, err := service.CreateCollection(ctx, userA, "Order", "", "public")
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	for i, id := range ids {
		if err := service.AddCollectionEntry(ctx, collection.ID, userA, id, int32(i)); err != nil {
			t.Fatalf("add entry: %v", err)
		}
	}

	order := func() []string {
		got, err := service.GetCollection(ctx, collection.ID, userA)
		if err != nil {
			t.Fatalf("get collection: %v", err)
		}
		out := make([]string, 0, len(got.Entries))
		for _, entry := range got.Entries {
			out = append(out, entry.ID)
		}
		return out
	}

	want := []string{ids[2], ids[0], ids[1]}
	// An id that is not a member is ignored, never added.
	if err := service.ReorderCollectionEntries(ctx, collection.ID, userA, []string{ids[2], outsider.ID, ids[0], ids[1]}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if got := order(); !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	if err := service.ReorderCollectionEntries(ctx, collection.ID, userB, ids); !errors.Is(err, catalog.ErrForbidden) {
		t.Fatalf("reorder by another user: %v, want ErrForbidden", err)
	}
}
