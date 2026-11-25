package spritesheet

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"factorio-expo/apps/expo-game-data/internal/prototypes"
)

func TestGenerateSheetsSharesCellsWithIdenticalPixels(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/unknown.png"), 64, 64, color.RGBA{R: 255, A: 255})
	writePNG(t, filepath.Join(root, "data/base/chest.png"), 64, 64, color.RGBA{G: 255, A: 255})
	catalog := prototypes.Catalog{}
	for _, prototypeType := range []string{"item", "fluid", "recipe", "virtual-signal"} {
		catalog.Prototypes = append(catalog.Prototypes, prototypes.Prototype{
			Type: prototypeType, Name: prototypeType + "-unknown", Icon: "__base__/unknown.png",
		})
	}
	catalog.Prototypes = append(catalog.Prototypes,
		prototypes.Prototype{Type: "item", Name: "steel-chest", Icon: "__base__/chest.png"},
		prototypes.Prototype{Type: "recipe", Name: "chest-a-recycling", Icon: "__base__/chest.png", Category: "recycling"},
		prototypes.Prototype{Type: "recipe", Name: "chest-b-recycling", Icon: "__base__/chest.png", Category: "recycling"},
	)
	sheets, err := GenerateSheets(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if got := sheets[CoreSheet].Manifest.Count; got != 2 {
		t.Fatalf("core cells = %d, want unknown and chest", got)
	}
	for _, sheetID := range []string{RecipesSheet, SignalsSheet, RecyclingSheet} {
		if got := sheets[sheetID].Manifest.Count; got != 0 {
			t.Fatalf("%s cells = %d, want every icon aliased into core", sheetID, got)
		}
	}
	unknown := lookup(t, sheets, "item", "item-unknown")
	for _, key := range [][2]string{{"fluid", "fluid-unknown"}, {"recipe", "recipe-unknown"}, {"virtual-signal", "virtual-signal-unknown"}} {
		if got := lookup(t, sheets, key[0], key[1]); got != unknown {
			t.Errorf("%s=%s = %v, want the shared unknown cell %v", key[0], key[1], got, unknown)
		}
	}
	chest := lookup(t, sheets, "item", "steel-chest")
	for _, name := range []string{"chest-a-recycling", "chest-b-recycling"} {
		if got := lookup(t, sheets, "recipe", name); got != chest {
			t.Errorf("%s = %v, want the core chest cell %v", name, got, chest)
		}
	}
}

func TestGenerateSheetsPutsFullSizeQualityIconsAndBadgesInCore(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/quality-normal.png"),
		64, 64, color.RGBA{G: 255, A: 255})
	for index, quality := range []string{"uncommon", "rare", "epic", "legendary"} {
		writePNG(t, filepath.Join(root, "data/quality/graphics/icons/quality-"+quality+".png"),
			64, 64, color.RGBA{R: uint8(100 + index), A: 255})
	}
	sheets, err := GenerateSheets(root, prototypes.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	core := sheets[CoreSheet]
	if core.Manifest.Count != 10 {
		t.Fatalf("core count = %d, want 10 quality cells", core.Manifest.Count)
	}
	for _, quality := range []string{"normal", "uncommon", "rare", "epic", "legendary"} {
		full := lookup(t, sheets, "quality", quality)
		badge := lookup(t, sheets, "quality-badge", quality+"-badge")
		if pixel(sheets, full, 0, 0).A == 0 || pixel(sheets, full, IconSize-1, IconSize-1).A == 0 {
			t.Fatalf("%s full-sized icon should fill its cell", quality)
		}
		if pixel(sheets, badge, 0, 0).A != 0 || pixel(sheets, badge, IconSize-1, IconSize-1).A != 0 {
			t.Fatalf("%s badge should leave the top-left and bottom-right corners transparent", quality)
		}
		// Factorio draws quality badges in the bottom-left corner.
		if pixel(sheets, badge, 3, IconSize-4).A == 0 {
			t.Fatalf("%s badge should occupy the bottom-left corner", quality)
		}
	}
}

func TestGenerateSheetsCopiesAnyQualitySignalIntoCore(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/core/graphics/icons/any-quality.png"),
		64, 64, color.RGBA{B: 255, A: 255})
	sheets, err := GenerateSheets(root, prototypes.Catalog{Prototypes: []prototypes.Prototype{
		{Type: "virtual-signal", Name: "signal-any-quality", Icon: "__core__/graphics/icons/any-quality.png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	quality := lookup(t, sheets, "quality", "any")
	if signal := lookup(t, sheets, "virtual-signal", "signal-any-quality"); signal != quality || quality[2] != CoreSheet {
		t.Fatalf("signal = %v, quality = %v; want one core cell", signal, quality)
	}
	if got := pixel(sheets, quality, 0, 0); got != (color.RGBA{B: 255, A: 255}) {
		t.Fatalf("any quality pixel = %v, want the signal source", got)
	}
	if sheets[SignalsSheet].Manifest.Count != 0 {
		t.Fatal("signals sheet should not draw the any-quality icon again")
	}
}

func TestGenerateSheetsIncludesNormalQualityWithoutQualityModule(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/quality-normal.png"),
		64, 64, color.RGBA{A: 255})
	sheets, err := GenerateSheets(root, prototypes.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	if got := sheets[CoreSheet].Manifest.Count; got != 2 {
		t.Fatalf("base-only quality cell count = %d, want full-size normal and its badge", got)
	}
}

func TestGenerateSheetsRejectsIncompleteQualityModuleIcons(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/quality/graphics/icons/quality-rare.png"),
		64, 64, color.RGBA{A: 255})
	if _, err := GenerateSheets(root, prototypes.Catalog{}); err == nil {
		t.Fatal("expected an error for incomplete quality module icons")
	}
}

func TestGenerateSheetsClassifiesAndDeduplicatesByName(t *testing.T) {
	root := t.TempDir()
	icons := map[string]color.RGBA{
		"pump-item": {R: 255, A: 255}, "pump-entity": {G: 255, A: 255}, "pump-recipe": {B: 255, A: 255},
		"recycling": {R: 200, A: 255}, "nauvis": {R: 1, A: 255}, "circuit": {R: 2, A: 255},
		"oil": {R: 3, A: 255}, "signal-A": {R: 4, A: 255}, "nauvis-orbit": {R: 5, A: 255},
	}
	for name, fill := range icons {
		writePNG(t, filepath.Join(root, "data/base/graphics/icons", name+".png"), 64, 64, fill)
	}
	catalog := prototypes.Catalog{Prototypes: []prototypes.Prototype{
		{Type: "recipe", Name: "offshore-pump", Icon: "__base__/graphics/icons/pump-recipe.png", Category: "crafting"},
		{Type: "assembling-machine", Name: "offshore-pump", Icon: "__base__/graphics/icons/pump-entity.png"},
		{Type: "entity", Name: "offshore-pump", Icon: "__base__/graphics/icons/pump-item.png"},
		{Type: "item", Name: "offshore-pump", Icon: "__base__/graphics/icons/pump-item.png", Group: "production"},
		{Type: "item", Name: "advanced-circuit", Icon: "__base__/graphics/icons/circuit.png", Group: "intermediates"},
		{Type: "recipe", Name: "advanced-circuit", Icon: "__base__/graphics/icons/circuit.png", Category: "crafting"},
		{Type: "recipe", Name: "advanced-oil-processing", Icon: "__base__/graphics/icons/oil.png", Category: "oil-processing"},
		{Type: "recipe", Name: "iron-plate", Icon: "__base__/graphics/icons/recycling.png", Category: "recycling"},
		{Type: "virtual-signal", Name: "offshore-pump", Icon: "__base__/graphics/icons/signal-A.png"},
		{Type: "space-location", Name: "nauvis-orbit", Icon: "__base__/graphics/icons/nauvis-orbit.png"},
		{Type: "planet", Name: "nauvis", Icon: "__base__/graphics/icons/nauvis.png"},
	}}
	sheets, err := GenerateSheets(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	core := sheets[CoreSheet]
	if got := []int{core.Manifest.Count, sheets[RecipesSheet].Manifest.Count, sheets[SignalsSheet].Manifest.Count, sheets[RecyclingSheet].Manifest.Count}; !reflect.DeepEqual(got, []int{2, 1, 3, 1}) {
		t.Fatalf("core/recipes/signals/recycling cells = %v, want [2 1 3 1]", got)
	}
	if got, want := core.Image.Bounds(), image.Rect(0, 0, 128, 64); got != want {
		t.Fatalf("core sheet bounds = %v, want %v", got, want)
	}
	pump := iconByName(t, core.Manifest, "offshore-pump")
	if pump.X != 64 || pump.Y != 0 || !reflect.DeepEqual(pump.Prototypes, []string{"item", "entity", "recipe"}) {
		t.Fatalf("offshore-pump = %+v, want item, entity and recipe at (64,0)", pump)
	}
	if got := pixel(sheets, lookup(t, sheets, "recipe", "offshore-pump"), 0, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("offshore pump pixel = %v, want the item icon", got)
	}
	for _, check := range []struct{ prototypeType, name, sheet string }{
		{"recipe", "advanced-circuit", CoreSheet},
		{"recipe", "advanced-oil-processing", RecipesSheet},
		{"recipe", "iron-plate", RecyclingSheet},
		{"virtual-signal", "offshore-pump", SignalsSheet},
		{"space-location", "nauvis-orbit", SignalsSheet},
		{"planet", "nauvis", SignalsSheet},
	} {
		if got := lookup(t, sheets, check.prototypeType, check.name)[2]; got != check.sheet {
			t.Errorf("%s=%s is on %v, want %s", check.prototypeType, check.name, got, check.sheet)
		}
	}
	typeScript, err := TypeScriptData(sheets)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		`"core":{"columns":2,"rows":1}`,
		`"recipes":{"columns":1,"rows":1}`,
		`"signals":{"columns":2,"rows":2}`,
		`"offshore-pump":[64,0,"core"]`,
		`"iron-plate":[0,0,"recycling"]`,
		`readonly [number, number, string]`,
	} {
		if !strings.Contains(string(typeScript), value) {
			t.Errorf("generated icon data does not contain %q", value)
		}
	}
}

func TestGenerateSheetsCreatesEmptySheetsWithoutSynthesizingSignals(t *testing.T) {
	sheets, err := GenerateSheets(t.TempDir(), prototypes.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sheetID := range sheetIDs {
		sheet := sheets[sheetID]
		if sheet.Manifest.Columns != 1 || sheet.Manifest.Rows != 1 || sheet.Manifest.Count != 0 ||
			len(sheet.Manifest.Icons) != 0 || sheet.Image.Bounds() != image.Rect(0, 0, IconSize, IconSize) {
			t.Fatalf("%s empty sheet = %+v with bounds %v; want valid empty 1x1 sheet", sheetID, sheet.Manifest, sheet.Image.Bounds())
		}
	}
}

func TestGenerateSheetsUsesOnlyRowsNeededForLastRow(t *testing.T) {
	root := t.TempDir()
	catalog := prototypes.Catalog{}
	for index := range 5 {
		name := fmt.Sprintf("item-%d", index)
		writePNG(t, filepath.Join(root, "data/base/graphics/icons", name+".png"), 64, 64, color.RGBA{R: uint8(index), A: 255})
		catalog.Prototypes = append(catalog.Prototypes, prototypes.Prototype{
			Type: "item", Name: name, Icon: "__base__/graphics/icons/" + name + ".png",
		})
	}
	sheets, err := GenerateSheets(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	core := sheets[CoreSheet]
	if core.Manifest.Columns != 3 || core.Manifest.Rows != 2 || core.Image.Bounds() != image.Rect(0, 0, 3*IconSize, 2*IconSize) {
		t.Fatalf("five-icon sheet = %dx%d grid, bounds %v; want 3x2", core.Manifest.Columns, core.Manifest.Rows, core.Image.Bounds())
	}
	if last := iconByName(t, core.Manifest, "item-4"); last.X != IconSize || last.Y != IconSize {
		t.Fatalf("last icon position = (%d,%d), want (%d,%d)", last.X, last.Y, IconSize, IconSize)
	}
}

func TestGenerateSheetsPadsImagesSmallerThanIconSize(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/small.png"), 32, 24, color.RGBA{R: 255, A: 255})
	sheets, err := GenerateSheets(root, prototypes.Catalog{Prototypes: []prototypes.Prototype{
		{Type: "item", Name: "small", Icon: "__base__/graphics/icons/small.png"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	small := lookup(t, sheets, "item", "small")
	if got := pixel(sheets, small, 0, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("small icon pixel = %v, want red pixel", got)
	}
	if got := pixel(sheets, small, 40, 40); got != (color.RGBA{}) {
		t.Fatalf("pixel outside small source = %v, want transparent padding", got)
	}
}

func TestGenerateSheetsCompositesScaledRecyclingIconLayers(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/recycler/graphics/icons/recycling.png"), 64, 64, color.RGBA{R: 255, A: 255})
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/iron-plate.png"), 64, 64, color.RGBA{G: 255, A: 255})
	sheets, err := GenerateSheets(root, prototypes.Catalog{Prototypes: []prototypes.Prototype{{
		Type: "recipe", Name: "iron-plate-recycling", Icon: "__base__/graphics/icons/iron-plate.png",
		IconLayers: []prototypes.IconLayer{
			{Icon: "__recycler__/graphics/icons/recycling.png"},
			{Icon: "__base__/graphics/icons/iron-plate.png", Scale: 0.4},
		},
		Category: "recycling",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	icon := lookup(t, sheets, "recipe", "iron-plate-recycling")
	if got := pixel(sheets, icon, 32, 32); got != (color.RGBA{G: 255, A: 255}) {
		t.Fatalf("composite center = %v, want scaled item icon over recycling background", got)
	}
	if got := pixel(sheets, icon, 8, 32); got != (color.RGBA{G: 255, A: 255}) {
		t.Fatalf("composite inset = %v, want enlarged item layer extending near cell edge", got)
	}
	if got := pixel(sheets, icon, 0, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("composite corner = %v, want recycling background", got)
	}
}

func TestGenerateSheetsEnlargesAndTintsFluidBarrelOverlay(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/fluid/barreling/barrel.png"), 64, 64, color.RGBA{R: 255, A: 255})
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/water.png"), 64, 64, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	sheets, err := GenerateSheets(root, prototypes.Catalog{Prototypes: []prototypes.Prototype{{
		Type: "recipe", Name: "water-barrel", Icon: "__base__/graphics/icons/fluid/barreling/barrel.png",
		IconLayers: []prototypes.IconLayer{
			{Icon: "__base__/graphics/icons/fluid/barreling/barrel.png"},
			{
				Icon: "__base__/graphics/icons/water.png", Scale: 0.25, Shift: []float64{-8, -8},
				Tint: prototypes.Color{Green: 0.34, Blue: 0.6, Alpha: 0.75, Set: true},
			},
		},
		Category: "crafting-with-fluid",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	icon := lookup(t, sheets, "recipe", "water-barrel")
	if got, want := pixel(sheets, icon, 8, 8), (color.RGBA{R: 64, G: 87, B: 153, A: 255}); got != want {
		t.Fatalf("tinted barrel inset = %v, want %v", got, want)
	}
	if got := pixel(sheets, icon, 5, 5); got != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("outside barrel inset = %v, want barrel layer", got)
	}
}

func TestGenerateSheetsKeepsRicherRecipeCompositeForSharedItemName(t *testing.T) {
	root := t.TempDir()
	for index, name := range []string{"base.png", "side.png", "top.png", "fluid.png"} {
		writePNG(t, filepath.Join(root, "data/base/graphics/icons", name), 64, 64, color.RGBA{R: uint8(index * 60), A: 255})
	}
	layers := []prototypes.IconLayer{
		{Icon: "__base__/graphics/icons/base.png"},
		{Icon: "__base__/graphics/icons/side.png"},
		{Icon: "__base__/graphics/icons/top.png"},
	}
	sheets, err := GenerateSheets(root, prototypes.Catalog{Prototypes: []prototypes.Prototype{
		{Type: "item", Name: "water-barrel", Icon: "__base__/graphics/icons/base.png", Group: "barrel", IconLayers: layers},
		{
			Type: "recipe", Name: "water-barrel", Icon: "__base__/graphics/icons/base.png", Category: "crafting-with-fluid",
			IconLayers: append(append([]prototypes.IconLayer{}, layers...), prototypes.IconLayer{Icon: "__base__/graphics/icons/fluid.png", Scale: 0.25}),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	core := sheets[CoreSheet]
	if core.Manifest.Count != 1 || len(core.Manifest.Icons) != 1 {
		t.Fatalf("core cells/lookups = %d/%d, want one shared cell", core.Manifest.Count, len(core.Manifest.Icons))
	}
	if got := pixel(sheets, lookup(t, sheets, "item", "water-barrel"), 32, 32); got != (color.RGBA{R: 180, A: 255}) {
		t.Fatalf("shared barrel center = %v, want the recipe's fluid layer", got)
	}
}

func TestGenerateSheetsPutsFluidsInCoreAndSharesSameNameRecipes(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/water.png"), 64, 64, color.RGBA{B: 255, A: 255})
	writePNG(t, filepath.Join(root, "data/base/graphics/icons/lubricant.png"), 64, 64, color.RGBA{G: 255, A: 255})
	sheets, err := GenerateSheets(root, prototypes.Catalog{Prototypes: []prototypes.Prototype{
		{Type: "fluid", Name: "water", Icon: "__base__/graphics/icons/water.png"},
		{Type: "fluid", Name: "lubricant", Icon: "__base__/graphics/icons/lubricant.png"},
		{Type: "recipe", Name: "lubricant", Icon: "__base__/graphics/icons/lubricant.png", Category: "chemistry"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	core := sheets[CoreSheet]
	if core.Manifest.Count != 2 || sheets[RecipesSheet].Manifest.Count != 0 {
		t.Fatalf("core/recipes cells = %d/%d, want 2/0", core.Manifest.Count, sheets[RecipesSheet].Manifest.Count)
	}
	if lubricant := iconByName(t, core.Manifest, "lubricant"); !reflect.DeepEqual(lubricant.Prototypes, []string{"recipe", "fluid"}) {
		t.Fatalf("lubricant prototypes = %v, want [recipe fluid]", lubricant.Prototypes)
	}
}

func TestGenerateSheetsReusesFluidResultIcons(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/water.png"), 64, 64, color.RGBA{B: 255, A: 255})
	writePNG(t, filepath.Join(root, "data/base/overlay.png"), 64, 64, color.RGBA{G: 255, A: 255})
	writePNG(t, filepath.Join(root, "data/base/recipe.png"), 64, 64, color.RGBA{R: 255, A: 255})
	catalog, err := prototypes.Extract(map[string]map[string]json.RawMessage{
		"fluid": {
			"water": json.RawMessage(`{"icons":[{"icon":"__base__/water.png"},{"icon":"__base__/overlay.png","scale":0.25}]}`),
		},
		"item": {
			"water": json.RawMessage(`{"icon":"__base__/recipe.png","stack_size":1}`),
		},
		"recipe": {
			"make-water": json.RawMessage(`{"results":[{"type":"fluid","name":"water","amount":10}],"category":"chemistry"}`),
			"explicit":   json.RawMessage(`{"icon":"__base__/recipe.png","results":[{"type":"fluid","name":"water","amount":10}]}`),
			"item-water": json.RawMessage(`{"results":[{"type":"item","name":"water","amount":1}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := GenerateSheets(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	water, recipe := lookup(t, sheets, "fluid", "water"), lookup(t, sheets, "recipe", "make-water")
	if water != recipe {
		t.Fatalf("recipe alias = %v, fluid = %v", recipe, water)
	}
	if got := pixel(sheets, water, 32, 32); got != (color.RGBA{G: 255, A: 255}) {
		t.Fatalf("fluid recipe composite center = %v, want green overlay", got)
	}
	for _, name := range []string{"explicit", "item-water"} {
		if got := pixel(sheets, lookup(t, sheets, "recipe", name), 0, 0); got != (color.RGBA{R: 255, A: 255}) {
			t.Fatalf("%s pixel = %v, want explicit/item red icon", name, got)
		}
	}
}

func TestGenerateSheetsAliasesFluidParametersToItems(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/base/water.png"), 64, 64, color.RGBA{B: 255, A: 255})
	catalog := prototypes.Catalog{Prototypes: []prototypes.Prototype{
		{Type: "fluid", Name: "water", Icon: "__base__/water.png"},
	}}
	for index := range 10 {
		name := fmt.Sprintf("parameter-%d", index)
		writePNG(t, filepath.Join(root, "data/base", name+".png"), 64, 64, color.RGBA{R: uint8(index), A: 255})
		catalog.Prototypes = append(catalog.Prototypes,
			prototypes.Prototype{Type: "item", Name: name, Icon: "__base__/" + name + ".png"},
			prototypes.Prototype{Type: "recipe", Name: name, Icon: "__base__/" + name + ".png"},
			prototypes.Prototype{Type: "fluid", Name: name, Icon: "__base__/missing.png"},
		)
	}
	catalog.Prototypes = append(catalog.Prototypes, prototypes.Prototype{
		Type: "recipe", Name: "make-parameter", Icon: "__base__/missing.png",
		FluidIcon: "parameter-3", Category: "chemistry",
	})
	sheets, err := GenerateSheets(root, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if got := sheets[CoreSheet].Manifest.Count; got != 11 {
		t.Fatalf("core cells = %d, want 10 parameters and water", got)
	}
	for index := range 10 {
		name := fmt.Sprintf("parameter-%d", index)
		item := lookup(t, sheets, "item", name)
		if item != lookup(t, sheets, "fluid", name) || item != lookup(t, sheets, "recipe", name) {
			t.Fatalf("%s aliases do not share item coordinates", name)
		}
	}
	if lookup(t, sheets, "recipe", "make-parameter") != lookup(t, sheets, "item", "parameter-3") {
		t.Fatal("fluid-result recipe did not reuse the parameter item icon")
	}
	for _, name := range []string{"parameter-10", "parameter-x", "water", ""} {
		if isParameterIcon(name) {
			t.Errorf("%q should not be treated as a parameter icon", name)
		}
	}
}

// lookup returns the [x, y, sheet] tuple the web app would read.
func lookup(t *testing.T, sheets map[string]GeneratedSheet, prototypeType, name string) [3]any {
	t.Helper()
	data, err := TypeScriptData(sheets)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "GAME_ICON_COORDINATES")
	start += strings.Index(text[start:], "= ") + 2
	var coordinates map[string]map[string][3]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimSpace(text[start:]), ";")), &coordinates); err != nil {
		t.Fatal(err)
	}
	value, exists := coordinates[prototypeType][name]
	if !exists {
		t.Fatalf("no %s=%s lookup", prototypeType, name)
	}
	return [3]any{int(value[0].(float64)), int(value[1].(float64)), value[2]}
}

func pixel(sheets map[string]GeneratedSheet, coordinates [3]any, dx, dy int) color.RGBA {
	return sheets[coordinates[2].(string)].Image.RGBAAt(coordinates[0].(int)+dx, coordinates[1].(int)+dy)
}

func iconByName(t *testing.T, manifest Manifest, name string) Icon {
	t.Helper()
	for _, icon := range manifest.Icons {
		if icon.Name == name {
			return icon
		}
	}
	t.Fatalf("icon %q not found", name)
	return Icon{}
}

func writePNG(t *testing.T, filePath string, width, height int, fill color.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	image := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			image.SetRGBA(x, y, fill)
		}
	}
	if err := png.Encode(file, image); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateSheetsPutsSpaceAgeLogoOnSignals(t *testing.T) {
	root := t.TempDir()
	writePNG(t, filepath.Join(root, "data/core/graphics/icons/space-age.png"),
		64, 64, color.RGBA{R: 200, B: 200, A: 255})
	sheets, err := GenerateSheets(root, prototypes.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	logo := lookup(t, sheets, "utility", "space-age")
	if logo[2] != SignalsSheet {
		t.Fatalf("space-age logo on %v, want the signals sheet", logo[2])
	}
	if got := pixel(sheets, logo, 0, 0); got != (color.RGBA{R: 200, B: 200, A: 255}) {
		t.Fatalf("space-age logo pixel = %v, want the core sprite", got)
	}
}
