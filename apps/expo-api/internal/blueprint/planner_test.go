package blueprint

import (
	"testing"
)

func TestParseDeconstructionPlannerBook(t *testing.T) {
	t.Parallel()
	root, err := Parse(readFixture(t, "planner-deconstruct-set.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if root.Kind != KindBook || len(root.Book.Children) != 13 {
		t.Fatalf("root = %s with %d children", root.Kind, len(root.Book.Children))
	}
	cliffs := root.Book.Children[0]
	if cliffs.Kind != KindDeconstructionPlanner || cliffs.Planner.Label != "Cliffs" {
		t.Fatalf("first child = %s %q", cliffs.Kind, cliffs.Planner.Label)
	}
	settings := cliffs.Planner.Settings.(DeconstructionSettings)
	if settings.EntityFilterMode != "whitelist" || settings.TileSelectionMode != "never" ||
		len(settings.EntityFilters) != 1 || settings.EntityFilters[0].Name != "cliff" {
		t.Fatalf("cliffs settings = %+v", settings)
	}
	trees := root.Book.Children[1].Planner.Settings.(DeconstructionSettings)
	if !trees.TreesAndRocksOnly {
		t.Fatalf("trees settings = %+v", trees)
	}

	// The planner string stands alone and parses back to the same planner.
	again, err := Parse(cliffs.Planner.ExchangeString, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if again.Kind != KindDeconstructionPlanner || again.Planner.Label != "Cliffs" {
		t.Fatalf("round trip = %s %q", again.Kind, again.Planner.Label)
	}

	summary := Summarize(root)
	if summary.PlannerCount != 13 || summary.BlueprintCount != 0 {
		t.Fatalf("summary counts = %d planners, %d blueprints", summary.PlannerCount, summary.BlueprintCount)
	}
}

func TestParseDeconstructionFilterModesAndTiles(t *testing.T) {
	t.Parallel()
	root, err := Parse(readFixture(t, "planner-deconstruct-set.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var blacklist, tiles bool
	for _, child := range root.Book.Children {
		settings := child.Planner.Settings.(DeconstructionSettings)
		if settings.EntityFilterMode == "blacklist" {
			blacklist = true
		}
		if len(settings.TileFilters) > 0 {
			tiles = true
			if settings.TileFilters[0].Name == "" {
				t.Fatalf("%q has an unnamed tile filter", child.Planner.Label)
			}
		}
	}
	if !blacklist || !tiles {
		t.Fatalf("blacklist = %v, tile filters = %v", blacklist, tiles)
	}
}

func TestParseUpgradePlannerBook(t *testing.T) {
	t.Parallel()
	root, err := Parse(readFixture(t, "planner-upgrade-set.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	exact := findPlanner(root, "Exactly rare to epic")
	if exact == nil {
		t.Fatal("no \"Exactly rare to epic\" planner")
	}
	if exact.Icons == "" {
		t.Fatal("expected planner icons from its settings")
	}
	mappers := exact.Settings.(UpgradeSettings).Mappers
	if len(mappers) == 0 {
		t.Fatal("no mappers")
	}
	first := mappers[0]
	if first.From == nil || first.To == nil || first.From.Comparator != "=" || first.To.Comparator != "" {
		t.Fatalf("first mapper = %+v -> %+v", first.From, first.To)
	}
	for i := 1; i < len(mappers); i++ {
		if mappers[i-1].Index > mappers[i].Index {
			t.Fatal("mappers are not in slot order")
		}
	}

	// A "Set" planner matches any source quality: no condition.
	if findPlannerWithAnyQuality(root) == nil {
		t.Fatal("no planner matching any quality")
	}
}

func findPlanner(node Node, label string) *Planner {
	if node.Kind.IsPlanner() && node.Planner.Label == label {
		return node.Planner
	}
	if node.Kind == KindBook {
		for _, child := range node.Book.Children {
			if found := findPlanner(child, label); found != nil {
				return found
			}
		}
	}
	return nil
}

func findPlannerWithAnyQuality(node Node) *Planner {
	if node.Kind == KindUpgradePlanner {
		for _, mapper := range node.Planner.Settings.(UpgradeSettings).Mappers {
			if mapper.From != nil && mapper.From.Quality == "" && mapper.From.Comparator == "" {
				return node.Planner
			}
		}
	}
	if node.Kind == KindBook {
		for _, child := range node.Book.Children {
			if found := findPlannerWithAnyQuality(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestExportBookWithPlannersRoundTrips(t *testing.T) {
	t.Parallel()
	root, err := Parse(readFixture(t, "planner-upgrade-set.txt"), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	exported, err := EncodeTree(exportOf(root), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(exported, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if Summarize(again).PlannerCount != Summarize(root).PlannerCount {
		t.Fatal("planner count changed in the export")
	}
}

func exportOf(node Node) ExportNode {
	switch {
	case node.Kind == KindBook:
		out := ExportNode{Kind: node.Kind, Label: node.Book.Label, Icons: node.Book.Icons}
		for _, child := range node.Book.Children {
			out.Children = append(out.Children, exportOf(child))
		}
		return out
	case node.Kind.IsPlanner():
		return ExportNode{Kind: node.Kind, ExchangeString: node.Planner.ExchangeString}
	default:
		return ExportNode{Kind: node.Kind, ExchangeString: node.Blueprint.ExchangeString}
	}
}

func TestSnapToGrid(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		blueprint map[string]any
		want      *SnapToGrid
	}{
		"none": {map[string]any{}, nil},
		"relative": {
			map[string]any{"snap-to-grid": map[string]any{"x": 32, "y": 16}},
			&SnapToGrid{Width: 32, Height: 16},
		},
		"absolute": {
			map[string]any{
				"snap-to-grid": map[string]any{"x": 50, "y": 50}, "absolute-snapping": true,
				"position-relative-to-grid": map[string]any{"x": 3, "y": 7},
			},
			&SnapToGrid{Width: 50, Height: 50, Absolute: true, OffsetX: 3, OffsetY: 7},
		},
	} {
		exchange, err := Encode(map[string]any{"blueprint": test.blueprint})
		if err != nil {
			t.Fatal(err)
		}
		node, err := Parse(exchange, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		got := node.Blueprint.SnapToGrid
		if (got == nil) != (test.want == nil) || (got != nil && *got != *test.want) {
			t.Errorf("%s: snap = %+v, want %+v", name, got, test.want)
		}
	}
}
