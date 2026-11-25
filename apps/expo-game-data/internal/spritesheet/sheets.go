package spritesheet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"

	"factorio-expo/apps/expo-game-data/internal/prototypes"
)

// Sheets split by how often a page needs them: core (items, entities,
// fluids, qualities) nearly always, the others only for their own icons
// (signals also holds the utility sprites, such as [space-age]).
const (
	CoreSheet      = "core"
	RecipesSheet   = "recipes"
	SignalsSheet   = "signals"
	RecyclingSheet = "recycling"
)

// sheetIDs is also the drawing order: a cell whose pixels match one on an
// earlier sheet reuses it instead of being drawn again.
var sheetIDs = []string{CoreSheet, RecipesSheet, SignalsSheet, RecyclingSheet}

type GeneratedSheet struct {
	Image    *image.RGBA
	Manifest Manifest
}

func GenerateSheets(root string, catalog prototypes.Catalog) (map[string]GeneratedSheet, error) {
	icons, err := qualityIcons(root)
	if err != nil {
		return nil, err
	}
	utility, err := utilityIcons(root)
	if err != nil {
		return nil, err
	}
	catalog.Prototypes = append(catalog.Prototypes, append(icons, utility...)...)
	coreNames := make(map[string]struct{})
	for _, prototype := range catalog.Prototypes {
		if category := categoryName(prototype); category != "" && category != "recipe" &&
			sheetForPrototype(prototype, nil) == CoreSheet {
			coreNames[prototype.Name] = struct{}{}
		}
	}
	grouped := make(map[string][]prototypes.Prototype, len(sheetIDs))
	for _, prototype := range catalog.Prototypes {
		if prototype.Type == "fluid" && isParameterIcon(prototype.Name) {
			continue
		}
		if prototype.Type == "recipe" && prototype.FluidIcon != "" && prototype.Category != "recycling" {
			continue
		}
		if sheetID := sheetForPrototype(prototype, coreNames); sheetID != "" {
			grouped[sheetID] = append(grouped[sheetID], prototype)
		}
		if prototype.Type == "virtual-signal" && prototype.Name == "signal-any-quality" {
			prototype.Type = "quality"
			prototype.Name = "any"
			grouped[CoreSheet] = append(grouped[CoreSheet], prototype)
		}
	}

	sheets := make(map[string]GeneratedSheet, len(sheetIDs))
	drawn := make(map[[32]byte]cellRef)
	for _, sheetID := range sheetIDs {
		sheet, manifest, err := generate(root, grouped[sheetID], sheetID, drawn)
		if err != nil {
			return nil, fmt.Errorf("generate %s icon sheet: %w", sheetID, err)
		}
		sheets[sheetID] = GeneratedSheet{Image: sheet, Manifest: manifest}
	}
	core := sheets[CoreSheet]
	if err := addFluidAliases(&core.Manifest, catalog.Prototypes); err != nil {
		return nil, err
	}
	sheets[CoreSheet] = core
	return sheets, nil
}

// addFluidAliases points fluid parameters at their item cells and recipes
// without their own icon at their fluid result's cell.
func addFluidAliases(core *Manifest, catalog []prototypes.Prototype) error {
	byType := func(prototypeType string) map[string]Icon {
		icons := make(map[string]Icon)
		for _, icon := range core.Icons {
			for _, candidate := range icon.Prototypes {
				if candidate == prototypeType {
					icons[icon.Name] = icon
				}
			}
		}
		return icons
	}
	items := byType("item")
	for _, prototype := range catalog {
		if prototype.Type != "fluid" || !isParameterIcon(prototype.Name) {
			continue
		}
		icon, exists := items[prototype.Name]
		if !exists {
			return fmt.Errorf("item icon missing for fluid parameter %q", prototype.Name)
		}
		core.Icons = append(core.Icons, Icon{Name: prototype.Name, Sheet: icon.Sheet, X: icon.X, Y: icon.Y, Prototypes: []string{"fluid"}})
	}
	fluids := byType("fluid")
	for _, prototype := range catalog {
		if prototype.Type != "recipe" || prototype.FluidIcon == "" || prototype.Category == "recycling" {
			continue
		}
		icon, exists := fluids[prototype.FluidIcon]
		if !exists {
			return fmt.Errorf("fluid icon %q missing for recipe %q", prototype.FluidIcon, prototype.Name)
		}
		core.Icons = append(core.Icons, Icon{Name: prototype.Name, Sheet: icon.Sheet, X: icon.X, Y: icon.Y, Prototypes: []string{"recipe"}})
	}
	return nil
}

func isParameterIcon(name string) bool {
	return len(name) == len("parameter-0") && name[:len("parameter-")] == "parameter-" &&
		name[len(name)-1] >= '0' && name[len(name)-1] <= '9'
}

// sheetForPrototype picks a prototype's sheet. Recipes go to core when they
// share a name with a core item, entity or fluid (one cell serves both);
// coreNames is nil while those names are being collected.
func sheetForPrototype(prototype prototypes.Prototype, coreNames map[string]struct{}) string {
	switch category := categoryName(prototype); {
	case category == "":
		return ""
	case category == "virtual-signal", category == "planet", category == "space-location",
		category == "utility":
		return SignalsSheet
	case category == "recipe" && prototype.Category == "recycling":
		return RecyclingSheet
	case category == "recipe":
		if _, shared := coreNames[prototype.Name]; shared {
			return CoreSheet
		}
		return RecipesSheet
	default:
		return CoreSheet
	}
}

func TypeScriptData(sheets map[string]GeneratedSheet) ([]byte, error) {
	type dimensions struct {
		Columns int `json:"columns"`
		Rows    int `json:"rows"`
	}
	grids := make(map[string]dimensions, len(sheetIDs))
	coordinates := make(map[string]map[string][3]any)
	for _, sheetID := range sheetIDs {
		sheet, exists := sheets[sheetID]
		if !exists {
			continue
		}
		grids[sheetID] = dimensions{Columns: sheet.Manifest.Columns, Rows: sheet.Manifest.Rows}
		for _, icon := range sheet.Manifest.Icons {
			for _, prototypeType := range icon.Prototypes {
				if coordinates[prototypeType] == nil {
					coordinates[prototypeType] = make(map[string][3]any)
				}
				coordinates[prototypeType][icon.Name] = [3]any{icon.X, icon.Y, icon.Sheet}
			}
		}
	}
	encodedSheets, err := json.Marshal(grids)
	if err != nil {
		return nil, fmt.Errorf("encode icon sheet dimensions: %w", err)
	}
	encodedCoordinates, err := json.Marshal(coordinates)
	if err != nil {
		return nil, fmt.Errorf("encode icon coordinates: %w", err)
	}
	var output bytes.Buffer
	output.WriteString("export const GAME_ICON_SHEETS: Record<string, { columns: number; rows: number }> = ")
	output.Write(encodedSheets)
	output.WriteString(";\nexport const GAME_ICON_COORDINATES: Record<string, Record<string, readonly [number, number, string]>> = ")
	output.Write(encodedCoordinates)
	output.WriteString(";\n")
	return output.Bytes(), nil
}
