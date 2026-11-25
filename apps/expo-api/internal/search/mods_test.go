package search

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// The entries document's mods field, from each leaf's stored detection
// against the embedded table.
func TestProjectionModsFilterValues(t *testing.T) {
	pool, service := newProjectionPool(t)
	ctx := context.Background()
	single := func(entities ...string) json.RawMessage {
		var list []map[string]any
		for i, name := range entities {
			list = append(list, map[string]any{
				"entity_number": i + 1, "name": name,
				"position": map[string]float64{"x": float64(i*10) + 0.5, "y": 0.5},
			})
		}
		blueprint := map[string]any{"item": "blueprint", "version": 562949958139904, "entities": list}
		encoded, _ := json.Marshal(map[string]any{"blueprint": blueprint})
		return encoded
	}
	book := func(leaves ...json.RawMessage) json.RawMessage {
		var blueprints []map[string]any
		for i, leaf := range leaves {
			var node map[string]any
			_ = json.Unmarshal(leaf, &node)
			node["index"] = i
			blueprints = append(blueprints, node)
		}
		encoded, _ := json.Marshal(map[string]any{"blueprint_book": map[string]any{
			"item": "blueprint-book", "version": 562949958139904, "blueprints": blueprints,
		}})
		return encoded
	}
	cases := map[string]struct {
		payload json.RawMessage
		want    []string
	}{
		"vanilla":            {single("transport-belt", "inserter"), []string{"vanilla"}},
		"space age":          {single("transport-belt", "foundry"), []string{"space-age"}},
		"elevated rails too": {single("rail-ramp"), []string{"space-age"}},
		"a listed mod":       {single("transport-belt", "aai-loader"), []string{"aai-loaders"}},
		"unknown names only": {single("e2e-mystery-machine"), []string{}},
		"book, one modded":   {book(single("transport-belt"), single("foundry", "aai-loader")), []string{"space-age", "aai-loaders"}},
	}
	for name, test := range cases {
		exchange, err := service.EncodeBlueprint(test.payload)
		if err != nil {
			t.Fatalf("%s: encode: %v", name, err)
		}
		entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange, Visibility: "public"})
		if err != nil {
			t.Fatalf("%s: create: %v", name, err)
		}
		entryID, _ := parseID(entry.ID)
		proj, err := buildProjection(ctx, service, db.New(pool), entryID)
		if err != nil {
			t.Fatalf("%s: project: %v", name, err)
		}
		if !reflect.DeepEqual(proj.Entry.Mods, test.want) {
			t.Errorf("%s: mods = %v, want %v", name, proj.Entry.Mods, test.want)
		}
	}
}
