package mods

import (
	"reflect"
	"testing"
)

const fixture = `{
  "factorio_version": "2.1.21",
  "modsets": [
    {"name": "base", "title": "Base Mod", "builtin": true, "mods": [], "entities": ["assembling-machine-1", "transport-belt"], "recipes": ["iron-gear-wheel"]},
    {"name": "elevated-rails", "title": "Elevated Rails", "builtin": true, "mods": [], "entities": ["rail-ramp"], "recipes": []},
    {"name": "space-age", "title": "Space Age", "builtin": true, "mods": [], "entities": ["foundry", "turbo-loader"], "recipes": ["casting-iron"]},
    {"name": "loaders", "title": "Loaders", "builtin": false, "mods": [], "entities": ["aai-loader", "turbo-loader"], "recipes": ["aai-loader"]},
    {"name": "overhaul", "title": "Overhaul", "builtin": false, "mods": [], "entities": ["aai-loader", "big-furnace"], "recipes": []}
  ]
}`

func newFixture(t *testing.T) *Table {
	t.Helper()
	table, err := New([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestEmbeddedTableLoads(t *testing.T) {
	table := Embedded()
	if table.Version() == "" {
		t.Fatal("embedded table has no version")
	}
	detection := table.Detect([]string{"transport-belt"}, []string{"iron-gear-wheel"})
	if len(detection.Modsets) != 0 || len(detection.UnknownEntities) != 0 {
		t.Fatalf("a base blueprint needs nothing, got %+v", detection)
	}
}

func TestDetectVanillaNeedsNothing(t *testing.T) {
	detection := newFixture(t).Detect([]string{"transport-belt", "transport-belt"}, []string{"iron-gear-wheel"})
	want := Detection{Modsets: []Match{}, UnknownEntities: []string{}, UnknownRecipes: []string{}}
	if !reflect.DeepEqual(detection, want) {
		t.Fatalf("got %+v, want %+v", detection, want)
	}
}

func TestDetectBuiltinsAreExactAndWinSharedNames(t *testing.T) {
	// turbo-loader is in space-age and loaders: the built-in takes it alone.
	detection := newFixture(t).Detect([]string{"foundry", "turbo-loader", "rail-ramp"}, []string{"casting-iron"})
	want := []Match{
		{Name: "elevated-rails", Title: "Elevated Rails", Builtin: true, Score: 1},
		{Name: "space-age", Title: "Space Age", Builtin: true, Score: 1},
	}
	if !reflect.DeepEqual(detection.Modsets, want) {
		t.Fatalf("got %+v, want %+v", detection.Modsets, want)
	}
}

func TestDetectScoresOtherModsetsAndListsUnknownNames(t *testing.T) {
	detection := newFixture(t).Detect(
		[]string{"aai-loader", "big-furnace", "mystery-machine", "foundry"},
		[]string{"aai-loader", "mystery-recipe"},
	)
	// Five non-vanilla names: aai-loader (entity, both mods), big-furnace
	// (overhaul), the aai-loader recipe (loaders) and two unknown ones.
	want := []Match{
		{Name: "space-age", Title: "Space Age", Builtin: true, Score: 1},
		{Name: "loaders", Title: "Loaders", Score: 0.4},
		{Name: "overhaul", Title: "Overhaul", Score: 0.4},
	}
	if !reflect.DeepEqual(detection.Modsets, want) {
		t.Fatalf("got %+v, want %+v", detection.Modsets, want)
	}
	if !reflect.DeepEqual(detection.UnknownEntities, []string{"mystery-machine"}) ||
		!reflect.DeepEqual(detection.UnknownRecipes, []string{"mystery-recipe"}) {
		t.Fatalf("unknown names = %v / %v", detection.UnknownEntities, detection.UnknownRecipes)
	}
}

func TestNewNeedsBase(t *testing.T) {
	if _, err := New([]byte(`{"factorio_version": "2.1.21", "modsets": []}`)); err == nil {
		t.Fatal("a table without base was accepted")
	}
}

func TestVersionFollowsContent(t *testing.T) {
	a := newFixture(t)
	b, err := New([]byte(fixture + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Version() == b.Version() {
		t.Fatal("different bytes share a version")
	}
}

func TestBadgesSpaceAgeFirstThenGuessesAboveThreshold(t *testing.T) {
	detection := Detection{Modsets: []Match{
		{Name: "space-age", Title: "Space Age", Builtin: true, Score: 1},
		{Name: "quality", Title: "Quality", Builtin: true, Score: 1},
		{Name: "likely", Title: "Likely Mod", Score: BadgeMinScore},
		{Name: "unlikely", Title: "Unlikely Mod", Score: BadgeMinScore - 0.01},
	}}
	got := detection.Badges()
	if len(got) != 2 || got[0] != SpaceAge || got[1] != "likely" {
		t.Fatalf("Badges() = %v, want [space-age likely]", got)
	}
	if got := (Detection{}).Badges(); got == nil || len(got) != 0 {
		t.Fatalf("vanilla Badges() = %v, want empty non-nil", got)
	}
}

func TestDefinesNamesOfEveryModset(t *testing.T) {
	table := newFixture(t)
	for _, name := range []string{"transport-belt", "foundry", "big-furnace"} {
		if !table.DefinesEntity(name) {
			t.Fatalf("entity %q not defined", name)
		}
	}
	if table.DefinesEntity("iron-gear-wheel") || table.DefinesEntity("nothing") {
		t.Fatal("a recipe or unknown name counts as an entity")
	}
	if !table.DefinesRecipe("casting-iron") || table.DefinesRecipe("foundry") {
		t.Fatal("recipe lookup mixed up")
	}
}
