package catalog_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
)

// A book of planners stores every planner with its settings and string,
// queues no preview or analysis work, and exports back to the same planners.
func TestCreateEntryStoresPlanners(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: strings.TrimSpace(readFixture(t, "planner-deconstruct-set.txt")),
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	root := entry.Revision.Root
	if root.Kind != "book" || len(root.Children) != 13 || len(root.Leaves()) != 0 {
		t.Fatalf("root = %s with %d children, %d leaves", root.Kind, len(root.Children), len(root.Leaves()))
	}
	if entry.Title != "Deconstruction set" {
		t.Fatalf("title = %q", entry.Title)
	}
	cliffs := root.Children[0]
	if cliffs.Kind != "deconstruction_planner" || cliffs.Label != "Cliffs" || cliffs.ExchangeString == "" {
		t.Fatalf("first child = %+v", cliffs)
	}
	if cliffs.Planner == nil || cliffs.Planner.Deconstruction == nil || cliffs.Planner.Upgrade != nil {
		t.Fatalf("planner = %+v", cliffs.Planner)
	}
	settings := cliffs.Planner.Deconstruction
	if len(settings.EntityFilters) != 1 || settings.EntityFilters[0].Name != "cliff" {
		t.Fatalf("settings = %+v", settings)
	}

	var jobs int
	if err := pool.QueryRow(ctx,
		"SELECT (SELECT count(*) FROM phase_jobs)",
	).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("planners queued %d jobs", jobs)
	}

	reread, err := service.GetEntry(ctx, entry.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reread.Revision.Root.Children[0].Planner, cliffs.Planner) {
		t.Fatal("a re-read returns different planner settings")
	}
	exported, err := service.ExportRevisionString(ctx, entry.ID, entry.Revision.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := blueprint.Parse(exported, blueprint.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if blueprint.Summarize(again).PlannerCount != 13 {
		t.Fatal("the export lost planners")
	}
}

// Any node exports on its own: a planner, a sub-book (re-encoded), and
// nothing for an unknown id.
func TestExportNodeString(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()

	entry, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: strings.TrimSpace(readFixture(t, "planner-upgrade-set.txt")),
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	var planner, book *catalog.Node
	for i := range entry.Revision.Root.Children {
		child := &entry.Revision.Root.Children[i]
		if planner == nil && child.Kind == "upgrade_planner" {
			planner = child
		}
		if book == nil && child.Kind == "book" {
			book = child
		}
	}
	if planner == nil || book == nil {
		t.Fatal("fixture has no top-level planner and sub-book")
	}

	exported, err := service.ExportNodeString(ctx, planner.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if exported != planner.ExchangeString {
		t.Fatal("a planner exports something other than its own string")
	}
	exported, err = service.ExportNodeString(ctx, book.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := blueprint.Parse(exported, blueprint.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Kind != blueprint.KindBook || parsed.Book.Label != book.Label || len(parsed.Book.Children) != len(book.Children) {
		t.Fatalf("sub-book exports as %s with %+v", parsed.Kind, parsed.Book)
	}
	if _, err := service.ExportNodeString(ctx, "aaaaaaaaaaaa", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown id: %v, want ErrNotFound", err)
	}
}

// A planner on its own is an entry too, titled by its label.
func TestCreateEntryFromSinglePlanner(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	book, err := blueprint.Parse(strings.TrimSpace(readFixture(t, "planner-upgrade-set.txt")), blueprint.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var planner *blueprint.Planner
	for _, child := range book.Book.Children {
		if child.Kind == blueprint.KindUpgradePlanner {
			planner = child.Planner
			break
		}
	}
	if planner == nil {
		t.Fatal("no top-level upgrade planner in the fixture")
	}

	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: planner.ExchangeString})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	root := entry.Revision.Root
	if root.Kind != "upgrade_planner" || entry.Title != planner.Label || root.ExchangeString != planner.ExchangeString {
		t.Fatalf("root = %s %q, title %q", root.Kind, root.Label, entry.Title)
	}
	cards, err := service.EntryCards(ctx, []string{entry.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	card, ok := cards[entry.ID]
	if !ok || card.Kind != "upgrade_planner" || card.Label != planner.Label || card.LeafCount != 0 {
		t.Fatalf("cards = %+v", cards)
	}
}

// Snap-to-grid is stored with the leaf and returned by its detail.
func TestLeafDetailSnapToGrid(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	exchange, err := blueprint.Encode(map[string]any{"blueprint": map[string]any{
		"entities":     []any{map[string]any{"entity_number": 1, "name": "wooden-chest", "position": map[string]any{"x": 0.5, "y": 0.5}}},
		"snap-to-grid": map[string]any{"x": 10, "y": 6}, "absolute-snapping": true,
		"position-relative-to-grid": map[string]any{"x": 2, "y": 1},
	}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := service.CreateEntry(ctx, catalog.Submission{Exchange: exchange})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetLeafDetail(ctx, entry.ID, entry.Revision.Root.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := blueprint.SnapToGrid{Width: 10, Height: 6, Absolute: true, OffsetX: 2, OffsetY: 1}
	if detail.SnapToGrid == nil || *detail.SnapToGrid != want {
		t.Fatalf("snap to grid = %+v", detail.SnapToGrid)
	}
}
