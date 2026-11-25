package search

import (
	"slices"
	"testing"

	"factorio-expo/apps/expo-api/internal/mods"
	"factorio-expo/apps/expo-game-data/communitynames"
)

// Every community name points at a real game recipe, so a renamed or
// misspelled item fails here instead of silently matching nothing.
func TestCommunityNamesPointAtRecipes(t *testing.T) {
	table := mods.Embedded()
	for _, entry := range communitynames.Names() {
		if !table.DefinesRecipe(entry.Item) {
			t.Errorf("community names: %q is no recipe of the pinned game", entry.Item)
		}
	}
}

func TestCommunitySynonymsGroupNamesWithTheItem(t *testing.T) {
	var red synonymItem
	for _, item := range communitySynonyms() {
		if item.ID == "automation-science-pack" {
			red = item
		}
	}
	for _, phrase := range []string{"red science", "automation science pack", "automation-science-pack"} {
		if !slices.Contains(red.Synonyms, phrase) {
			t.Errorf("red science synonym %v lacks %q", red.Synonyms, phrase)
		}
	}
}
