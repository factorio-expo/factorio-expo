package catalog_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/mods"
)

const modTable = `{
  "factorio_version": "2.1.21",
  "modsets": [
    {"name": "base", "title": "Base Mod", "builtin": true, "mods": [], "entities": ["transport-belt"], "recipes": []},
    {"name": "space-age", "title": "Space Age", "builtin": true, "mods": [], "entities": ["foundry"], "recipes": []},
    {"name": "loaders", "title": "Loaders", "builtin": false, "mods": [], "entities": ["aai-loader"], "recipes": []}
  ]
}`

func modsTable(t *testing.T, data string) *mods.Table {
	t.Helper()
	table, err := mods.New([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func moddedExchange(t *testing.T, service *catalog.Service) string {
	t.Helper()
	payload := json.RawMessage(`{"blueprint": {"item": "blueprint", "version": 562949958139904, "entities": [
		{"entity_number": 1, "name": "transport-belt", "position": {"x": 0.5, "y": 0.5}},
		{"entity_number": 2, "name": "foundry", "position": {"x": 3.5, "y": 3.5}},
		{"entity_number": 3, "name": "aai-loader", "position": {"x": 8.5, "y": 0.5}},
		{"entity_number": 4, "name": "mystery-machine", "position": {"x": 10.5, "y": 0.5}}
	]}}`)
	exchange, err := service.EncodeBlueprint(payload)
	if err != nil {
		t.Fatal(err)
	}
	return exchange
}

func TestUploadStoresModDetectionAndInspectReturnsIt(t *testing.T) {
	service, _ := newServiceWith(t, catalog.Options{Mods: modsTable(t, modTable)})
	ctx := context.Background()
	exchange := moddedExchange(t, service)
	want := mods.Detection{
		Modsets: []mods.Match{
			{Name: "space-age", Title: "Space Age", Builtin: true, Score: 1},
			{Name: "loaders", Title: "Loaders", Score: 0.5},
		},
		UnknownEntities: []string{"mystery-machine"},
		UnknownRecipes:  []string{},
	}

	inspection, err := service.InspectBlueprint(ctx, exchange)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspection.Mods, want) {
		t.Fatalf("inspect mods = %+v, want %+v", inspection.Mods, want)
	}

	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange})
	if err != nil {
		t.Fatalf("a modded upload must be accepted: %v", err)
	}
	leaf := entry.Revision.Root.Leaves()[0]
	detail, err := service.GetLeafDetail(ctx, entry.ID, leaf.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Mods == nil || !reflect.DeepEqual(*detail.Mods, want) {
		t.Fatalf("stored mods = %+v, want %+v", detail.Mods, want)
	}
}

func TestRedetectModsUpdatesLeavesOfAnOlderTable(t *testing.T) {
	old, pool := newServiceWith(t, catalog.Options{Mods: modsTable(t, modTable)})
	ctx := context.Background()
	entry, err := old.CreateEntry(ctx, catalog.Submission{Exchange: moddedExchange(t, old)})
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := old.RedetectMods(ctx); err != nil || updated != 0 {
		t.Fatalf("same table re-detected %d leaves, %v", updated, err)
	}

	// The new table knows the mystery machine.
	newer := catalog.New(pool, catalog.Options{Mods: modsTable(t, `{
	  "factorio_version": "2.1.21",
	  "modsets": [
	    {"name": "base", "title": "Base Mod", "builtin": true, "mods": [], "entities": ["transport-belt"], "recipes": []},
	    {"name": "space-age", "title": "Space Age", "builtin": true, "mods": [], "entities": ["foundry"], "recipes": []},
	    {"name": "loaders", "title": "Loaders", "builtin": false, "mods": [], "entities": ["aai-loader", "mystery-machine"], "recipes": []}
	  ]
	}`)})
	if _, err := pool.Exec(ctx, "DELETE FROM entry_search_jobs"); err != nil {
		t.Fatal(err)
	}
	if updated, err := newer.RedetectMods(ctx); err != nil || updated != 1 {
		t.Fatalf("re-detected %d leaves, %v; want 1", updated, err)
	}
	// Search filters on the detection, so the entry is queued for re-indexing.
	var queued int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM entry_search_jobs WHERE entry_id = $1 AND action = 'index'",
		entry.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("queued search jobs = %d, %v; want 1", queued, err)
	}
	leaf := entry.Revision.Root.Leaves()[0]
	detail, err := newer.GetLeafDetail(ctx, entry.ID, leaf.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []mods.Match{
		{Name: "space-age", Title: "Space Age", Builtin: true, Score: 1},
		{Name: "loaders", Title: "Loaders", Score: 1},
	}
	if !reflect.DeepEqual(detail.Mods.Modsets, want) || len(detail.Mods.UnknownEntities) != 0 {
		t.Fatalf("re-detected mods = %+v", detail.Mods)
	}
}
