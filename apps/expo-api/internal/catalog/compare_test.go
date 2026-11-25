package catalog_test

import (
	"context"
	"errors"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// Only a blueprint of a public, non-quick-preview entry can be compared.
func TestCompareLeafOnlyPublicBlueprints(t *testing.T) {
	service, _, user, _ := newSocialFixtures(t)
	ctx := context.Background()
	exchange := readFixture(t, "bp-starter-smelter.txt")

	public, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: user, Visibility: "public"})
	if err != nil {
		t.Fatal(err)
	}
	leaf := public.Revision.Root.Leaves()[0]
	got, err := service.CompareLeaf(ctx, leaf.ID)
	if err != nil {
		t.Fatalf("compare a public leaf: %v", err)
	}
	if got.ID != leaf.ID || got.EntryID != public.ID || got.Production == nil || got.ModBadges == nil {
		t.Fatalf("compare leaf = %+v", got)
	}

	unlisted, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, OwnerUserID: user, Visibility: "unlisted"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.CreateQuickPreview(ctx, exchange, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, entry := range map[string]catalog.Entry{"unlisted": unlisted, "quick preview": preview} {
		if _, err := service.CompareLeaf(ctx, entry.Revision.Root.Leaves()[0].ID); !errors.Is(err, catalog.ErrNotFound) {
			t.Errorf("%s: err = %v, want not found", name, err)
		}
	}
	for _, id := range []string{"abcdefghijkl", "not an id", public.ID} {
		if _, err := service.CompareLeaf(ctx, id); !errors.Is(err, catalog.ErrNotFound) {
			t.Errorf("%q: err = %v, want not found", id, err)
		}
	}
}
