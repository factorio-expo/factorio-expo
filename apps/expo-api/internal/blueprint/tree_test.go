package blueprint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIconsUseFactorioRichText(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(map[string]any{
		"blueprint": map[string]any{
			"icons": []map[string]any{
				{"signal": map[string]string{"type": "virtual", "name": "signal-A"}, "index": 2},
				{"signal": map[string]string{"type": "item", "name": "iron-plate"}, "index": 1},
			},
			"entities": []any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	exchange, err := Encode(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	node, err := Parse(exchange, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := node.Blueprint.Icons, "[item=iron-plate][virtual-signal=signal-A]"; got != want {
		t.Fatalf("icons = %q, want %q", got, want)
	}
}

// readFixture loads one of the repository's real exchange strings.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "test-data", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}

func TestParseSingleBlueprint(t *testing.T) {
	t.Parallel()
	node, err := Parse(readFixture(t, "bp-starter-smelter.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != KindBlueprint {
		t.Fatalf("kind = %q", node.Kind)
	}
	if node.Blueprint.EntityCount == 0 {
		t.Fatal("expected a non-zero entity count")
	}
	if node.Blueprint.Icons == "" {
		t.Fatal("expected Factorio rich-text icons")
	}
	leaves := collectLeaves(node)
	if len(leaves) != 1 {
		t.Fatalf("leaves = %d", len(leaves))
	}
	// A leaf string must round-trip back into a blueprint on its own.
	again, err := Parse(leaves[0].ExchangeString, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if again.Kind != KindBlueprint || again.Blueprint.EntityCount != node.Blueprint.EntityCount {
		t.Fatalf("round trip mismatch: %#v", again.Blueprint)
	}
}

func TestParseBookProducesRenderableLeaves(t *testing.T) {
	t.Parallel()
	node, err := Parse(readFixture(t, "book-starter-kit.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != KindBook {
		t.Fatalf("kind = %q", node.Kind)
	}
	leaves := collectLeaves(node)
	if len(leaves) < 2 {
		t.Fatalf("leaves = %d", len(leaves))
	}
	for _, leaf := range leaves {
		child, err := Parse(leaf.ExchangeString, DefaultLimits())
		if err != nil {
			t.Fatalf("leaf %q: %v", leaf.Label, err)
		}
		if child.Kind != KindBlueprint {
			t.Fatalf("leaf %q decoded as %q", leaf.Label, child.Kind)
		}
	}
}

func TestParseRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"empty":          "",
		"bad marker":     "1abc",
		"not base64":     "0!!!!",
		"not compressed": "0aGVsbG8=",
	} {
		if _, err := Parse(value, DefaultLimits()); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestParseEnforcesLeafLimit(t *testing.T) {
	t.Parallel()
	limits := DefaultLimits()
	limits.MaxLeaves = 1
	if _, err := Parse(readFixture(t, "book-starter-kit.txt"), limits); err == nil {
		t.Fatal("expected a limit error")
	}
}

func TestParseSummarizesEntitiesAndRecipes(t *testing.T) {
	t.Parallel()
	node, err := Parse(readFixture(t, "bp-science-clean.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if node.Kind != KindBlueprint {
		t.Fatalf("kind = %q", node.Kind)
	}
	var assemblerCount int32
	for _, entity := range node.Blueprint.Entities {
		if entity.Name == "assembling-machine-2" {
			assemblerCount = entity.Count
		}
	}
	if assemblerCount != 6 {
		t.Fatalf("assembling-machine-2 count = %d, want 6", assemblerCount)
	}
	// Entities must be sorted for deterministic storage.
	for i := 1; i < len(node.Blueprint.Entities); i++ {
		previous, current := node.Blueprint.Entities[i-1], node.Blueprint.Entities[i]
		if previous.Name > current.Name ||
			(previous.Name == current.Name && previous.Quality >= current.Quality) {
			t.Fatalf("entities not sorted at index %d: %#v", i, node.Blueprint.Entities)
		}
	}
}

func TestSummarizeEntitiesSeparatesQuality(t *testing.T) {
	t.Parallel()
	raw := []json.RawMessage{
		json.RawMessage(`{"name":"biolab","quality":"normal","recipe":"automation-science-pack","recipe_quality":"normal"}`),
		json.RawMessage(`{"name":"biolab","quality":"normal","recipe":"automation-science-pack","recipe_quality":"normal"}`),
		json.RawMessage(`{"name":"biolab","quality":"legendary","recipe":"automation-science-pack","recipe_quality":"rare"}`),
	}
	entities, recipes := summarizeEntities(raw)
	if len(entities) != 2 || entities[0] != (EntityStat{Name: "biolab", Quality: "legendary", Count: 1}) ||
		entities[1] != (EntityStat{Name: "biolab", Quality: "normal", Count: 2}) {
		t.Fatalf("entities = %#v, want legendary x1 and normal x2", entities)
	}
	if len(recipes) != 2 ||
		recipes[0] != (RecipeStat{Name: "automation-science-pack", Quality: "normal", Count: 2}) ||
		recipes[1] != (RecipeStat{Name: "automation-science-pack", Quality: "rare", Count: 1}) {
		t.Fatalf("recipes = %#v, want normal x2 and rare x1", recipes)
	}
}

// collectLeaves returns every blueprint of a tree in depth-first source order.
func collectLeaves(node Node) []*Blueprint {
	if node.Kind == KindBlueprint {
		return []*Blueprint{node.Blueprint}
	}
	var leaves []*Blueprint
	for index := range node.Book.Children {
		leaves = append(leaves, collectLeaves(node.Book.Children[index])...)
	}
	return leaves
}
