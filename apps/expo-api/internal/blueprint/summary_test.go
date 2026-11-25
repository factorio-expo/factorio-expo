package blueprint

import (
	"encoding/json"
	"testing"
)

func encodeForTest(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	exchange, err := Encode(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	return exchange
}

// A book's summary is its own label and the totals of all its leaves.
func TestSummarizeBookTotalsLeaves(t *testing.T) {
	t.Parallel()
	leaf := func(label string, entities ...map[string]any) map[string]any {
		list := make([]any, len(entities))
		for i, entity := range entities {
			list[i] = entity
		}
		return map[string]any{"blueprint": map[string]any{"label": label, "entities": list}}
	}
	refinery := map[string]any{"name": "oil-refinery", "recipe": "advanced-oil-processing"}
	exchange := encodeForTest(t, map[string]any{"blueprint_book": map[string]any{
		"label":       "Oil",
		"description": "All of it",
		"version":     562949954404356,
		"blueprints": []any{
			leaf("A", refinery, map[string]any{"name": "pipe"}),
			map[string]any{"blueprint_book": map[string]any{"blueprints": []any{
				leaf("B", refinery, map[string]any{"name": "pipe", "quality": "rare"}),
			}}},
		},
	}})
	root, err := Parse(exchange, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	summary := Summarize(root)
	if summary.Kind != KindBook || summary.Label != "Oil" || summary.Description != "All of it" {
		t.Fatalf("root = %+v", summary)
	}
	if summary.GameVersion != "2.0.15" || summary.BlueprintCount != 2 || summary.EntityCount != 4 {
		t.Fatalf("counts = %+v", summary)
	}
	wantEntities := []EntityStat{
		{Name: "oil-refinery", Quality: "normal", Count: 2},
		{Name: "pipe", Quality: "normal", Count: 1},
		{Name: "pipe", Quality: "rare", Count: 1},
	}
	if len(summary.Entities) != len(wantEntities) {
		t.Fatalf("entities = %+v", summary.Entities)
	}
	for i, want := range wantEntities {
		if summary.Entities[i] != want {
			t.Fatalf("entities[%d] = %+v, want %+v", i, summary.Entities[i], want)
		}
	}
	if len(summary.Recipes) != 1 || summary.Recipes[0] != (RecipeStat{Name: "advanced-oil-processing", Quality: "normal", Count: 2}) {
		t.Fatalf("recipes = %+v", summary.Recipes)
	}
}

func TestSummarizeFixtureBlueprint(t *testing.T) {
	t.Parallel()
	root, err := Parse(readFixture(t, "bp-starter-smelter.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	summary := Summarize(root)
	if summary.Kind != KindBlueprint || summary.Label != "Starter smelter" ||
		summary.BlueprintCount != 1 || summary.EntityCount != 38 {
		t.Fatalf("summary = %+v", summary)
	}
}
