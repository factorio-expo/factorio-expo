package catalog_test

import (
	"context"
	"testing"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
)

// A node gets its card from the live entry: facts the source left out are
// the entry's, and a node whose entry left the listing, or a blueprint that
// is no longer in the current revision, has no card.
func TestNodeCards(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: readFixture(t, "bp-starter-smelter.txt")})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	leaf := entry.Revision.Root.Leaves()[0]
	nodes := []catalog.CardNode{
		{EntryID: entry.ID, NodeID: leaf.ID, Kind: blueprint.KindBlueprint, Title: "From a board"},
		{EntryID: entry.ID, NodeID: "aaaaaaaaaaaa", Kind: blueprint.KindBlueprint, Title: "Replaced"},
	}

	cards, err := service.NodeCards(ctx, nodes, "")
	if err != nil {
		t.Fatalf("node cards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1: a blueprint outside the current revision has none", len(cards))
	}
	card := cards[0]
	if card.Title != "From a board" || card.GameVersion != entry.Revision.GameVersion ||
		!card.CreatedAt.Equal(entry.CreatedAt) || card.Tags == nil || card.ModBadges == nil || card.Records == nil {
		t.Fatalf("card = %+v, want the node's title and the entry's version, dates and empty lists", card)
	}

	if err := service.DeleteEntry(ctx, entry.ID, catalog.Authorization{EditToken: entry.EditToken}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if cards, err = service.NodeCards(ctx, nodes[:1], ""); err != nil || len(cards) != 0 {
		t.Fatalf("cards of a deleted entry = %d (%v), want none", len(cards), err)
	}
}
