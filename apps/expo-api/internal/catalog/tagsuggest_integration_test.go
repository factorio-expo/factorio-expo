package catalog_test

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/blueprint"
)

// The seeded rules, through inspect: three refineries on two oil recipes and a
// pumpjack pair score oil-processing 4 (refinery, pumpjack, two recipes);
// heavy-oil-cracking alone gives coal-liquefaction 1, below the default
// minimum of 2.
func TestInspectSuggestsTagsFromSeededRules(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	if err := service.LoadTagSuggestionRules(ctx); err != nil {
		t.Fatal(err)
	}
	entities := []map[string]any{
		{"entity_number": 1, "name": "oil-refinery", "position": map[string]any{"x": 0, "y": 0}, "recipe": "advanced-oil-processing"},
		{"entity_number": 2, "name": "oil-refinery", "position": map[string]any{"x": 6, "y": 0}, "recipe": "advanced-oil-processing"},
		{"entity_number": 3, "name": "oil-refinery", "position": map[string]any{"x": 12, "y": 0}, "recipe": "heavy-oil-cracking"},
		{"entity_number": 4, "name": "pumpjack", "position": map[string]any{"x": 0, "y": 8}},
		{"entity_number": 5, "name": "pumpjack", "position": map[string]any{"x": 4, "y": 8}},
	}
	exchange, err := blueprint.Encode(map[string]any{"blueprint": map[string]any{
		"item": "blueprint", "label": "Oil", "version": 562949954404356, "entities": entities,
	}})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := service.InspectBlueprint(ctx, exchange)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspection.SuggestedTags, []string{"oil-processing"}) {
		t.Fatalf("suggested tags = %v, want [oil-processing]", inspection.SuggestedTags)
	}
	want := []blueprint.RecipeStat{
		{Name: "advanced-oil-processing", Quality: "normal", Count: 2},
		{Name: "heavy-oil-cracking", Quality: "normal", Count: 1},
	}
	if !reflect.DeepEqual(inspection.Recipes, want) {
		t.Fatalf("recipes = %+v, want %+v", inspection.Recipes, want)
	}
}

// The space platform fixtures get a ship tag of their own: the size class and
// armament rules score the hub plus one fact, and a platform's turrets and
// furnaces must not crowd them out.
func TestInspectSuggestsShipTagsForPlatforms(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	if err := service.LoadTagSuggestionRules(ctx); err != nil {
		t.Fatal(err)
	}
	// The hauler is a stand-in that will be swapped for another ship: any
	// ship tag will do for it.
	ships := map[string]string{
		"ship-hauler":     "",
		"mod-kr-platform": "small-ship",
	}
	shipTags := map[string]bool{
		"starter-ship": true, "small-ship": true, "fast-ship": true, "cargo-ship": true, "large-cargo-ship": true,
		"science-ship": true, "inner-planet-ship": true, "aquilo-level-ship": true, "solar-system-edge-ship": true,
		"promethean-ship": true,
	}
	for fixture, want := range ships {
		exchange, err := os.ReadFile("../../../../test-data/" + fixture + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		inspection, err := service.InspectBlueprint(ctx, strings.TrimSpace(string(exchange)))
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		if want != "" && !slices.Contains(inspection.SuggestedTags, want) {
			t.Errorf("%s: suggested tags %v lack %s", fixture, inspection.SuggestedTags, want)
		}
		ship := 0
		for _, tag := range inspection.SuggestedTags {
			if shipTags[tag] {
				ship++
			}
		}
		if ship == 0 {
			t.Errorf("%s: no ship tag in %v", fixture, inspection.SuggestedTags)
		}
	}
}
