package prototypes

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
)

type Prototype struct {
	Type       string      `json:"type"`
	Name       string      `json:"name"`
	Icon       string      `json:"icon"`
	IconLayers []IconLayer `json:"icon_layers,omitempty"`
	Group      string      `json:"group,omitempty"`
	Category   string      `json:"category,omitempty"`
	FluidIcon  string      `json:"fluid_icon,omitempty"`
}

type IconLayer struct {
	Icon  string    `json:"icon"`
	Scale float64   `json:"scale,omitempty"`
	Shift []float64 `json:"shift,omitempty"`
	Size  int       `json:"icon_size,omitempty"`
	Tint  Color     `json:"tint"`
}

type Color struct {
	Red   float64 `json:"r"`
	Green float64 `json:"g"`
	Blue  float64 `json:"b"`
	Alpha float64 `json:"a"`
	Set   bool    `json:"-"`
}

func (color *Color) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '[' {
		var components []float64
		if err := json.Unmarshal(data, &components); err != nil {
			return err
		}
		if len(components) < 3 || len(components) > 4 {
			return fmt.Errorf("tint array must have 3 or 4 components")
		}
		color.Red, color.Green, color.Blue, color.Alpha = components[0], components[1], components[2], 1
		if len(components) == 4 {
			color.Alpha = components[3]
		}
		color.Set = true
		return nil
	}

	var components map[string]json.RawMessage
	if err := json.Unmarshal(data, &components); err != nil {
		return err
	}
	*color = Color{Red: 1, Green: 1, Blue: 1, Alpha: 1, Set: true}
	for key, destination := range map[string]*float64{
		"r": &color.Red, "g": &color.Green, "b": &color.Blue, "a": &color.Alpha,
	} {
		if value, exists := components[key]; exists {
			if err := json.Unmarshal(value, destination); err != nil {
				return err
			}
		}
	}
	return nil
}

type Catalog struct {
	Prototypes []Prototype
}

type rawPrototype map[string]json.RawMessage

type itemSubgroup struct {
	Group string `json:"group"`
}

// Dump is data.raw as --dump-data wrote it: prototype type -> name -> fields.
type Dump map[string]map[string]json.RawMessage

// ReadDump decodes a data-raw-dump.json file.
func ReadDump(path string) (Dump, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Factorio data dump %q: %w", path, err)
	}
	defer file.Close()

	var dump Dump
	if err := json.NewDecoder(file).Decode(&dump); err != nil {
		return nil, fmt.Errorf("decode Factorio data dump %q: %w", path, err)
	}
	return dump, nil
}

// Extract collects the icon-bearing prototypes the sprite sheets draw.
func Extract(dump Dump) (Catalog, error) {
	var blacklistedNames = []string{
		"red-wire", "green-wire", "copper-wire", "copy-paste-tool",
		"simple-entity-with-force", "simple-entity-with-owner",
		"artillery-targeting-remote", "discharge-defense-remote", "empty-module-slot", "selection-tool",
		"spidertron-remote", "cut-paste-tool", "science",
		"spidertron-rocket-launcher-1", "spidertron-rocket-launcher-2",
		"spidertron-rocket-launcher-3", "spidertron-rocket-launcher-4",
	}

	subgroups := make(map[string]string)
	for name, encoded := range dump["item-subgroup"] {
		var subgroup itemSubgroup
		if err := json.Unmarshal(encoded, &subgroup); err != nil {
			return Catalog{}, fmt.Errorf("decode item subgroup %q: %w", name, err)
		}
		subgroups[name] = subgroup.Group
	}

	itemIcons := make(map[string]string)
	fluidIcons := make(map[string]string)
	itemNames := make(map[string]struct{})
	for prototypeType, named := range dump {
		for name, encoded := range named {
			if slices.Contains(blacklistedNames, name) {
				continue
			}
			var prototype rawPrototype
			if err := json.Unmarshal(encoded, &prototype); err != nil {
				return Catalog{}, fmt.Errorf("decode %s prototype %q: %w", prototypeType, name, err)
			}
			if !isItem(prototypeType, prototype) && prototypeType != "fluid" {
				continue
			}
			icon, err := readIcon(encoded)
			if err != nil {
				return Catalog{}, fmt.Errorf("decode %s prototype %q: %w", prototypeType, name, err)
			}
			if icon != "" {
				if prototypeType == "fluid" {
					fluidIcons[name] = icon
				} else {
					itemIcons[name] = icon
				}
				if isItem(prototypeType, prototype) {
					itemNames[name] = struct{}{}
				}
			}
		}
	}

	var catalog Catalog
	for prototypeType, named := range dump {
		names := make([]string, 0, len(named))
		for name := range named {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if slices.Contains(blacklistedNames, name) {
				continue
			}

			var prototype rawPrototype
			if err := json.Unmarshal(named[name], &prototype); err != nil {
				return Catalog{}, fmt.Errorf("decode %s prototype %q: %w", prototypeType, name, err)
			}
			icon, err := readIcon(named[name])
			if err != nil {
				return Catalog{}, fmt.Errorf("decode %s prototype %q: %w", prototypeType, name, err)
			}

			switch {
			case isItem(prototypeType, prototype):
				if icon == "" {
					continue
				}
				var fields struct {
					Group    string `json:"group"`
					Subgroup string `json:"subgroup"`
				}
				if err := json.Unmarshal(named[name], &fields); err != nil {
					return Catalog{}, fmt.Errorf("decode item fields for %q: %w", name, err)
				}
				group := fields.Group
				if group == "" {
					group = subgroups[fields.Subgroup]
				}
				if group == "" {
					group = "ungrouped"
				}
				iconLayers, err := readIconLayers(named[name])
				if err != nil {
					return Catalog{}, fmt.Errorf("decode icon layers for item %q: %w", name, err)
				}
				catalog.Prototypes = append(catalog.Prototypes, Prototype{
					Type: "item", Name: name, Icon: icon, IconLayers: iconLayers, Group: group,
				})
			case prototypeType == "recipe":
				var fields struct {
					Category    string          `json:"category"`
					Categories  []string        `json:"categories"`
					Result      string          `json:"result"`
					Results     json.RawMessage `json:"results"`
					Ingredients json.RawMessage `json:"ingredients"`
				}
				if err := json.Unmarshal(named[name], &fields); err != nil {
					return Catalog{}, fmt.Errorf("decode recipe fields for %q: %w", name, err)
				}
				fluidIcon := ""
				if icon == "" {
					result, err := firstRecipeResult(fields.Results)
					if err != nil {
						return Catalog{}, fmt.Errorf("decode recipe results for %q: %w", name, err)
					}
					if result.Name == "" {
						result.Name = fields.Result
					}
					if result.Type != "fluid" {
						icon = itemIcons[result.Name]
					}
					if result.Type == "fluid" || (result.Type == "" && icon == "") {
						icon = fluidIcons[result.Name]
						if icon != "" {
							fluidIcon = result.Name
						}
					}
				}
				if hasRecyclingCategory(fields.Categories, fields.Category) {
					if recyclingItem := firstItemIngredient(fields.Ingredients); recyclingItem != "" {
						if itemIcon := itemIcons[recyclingItem]; itemIcon != "" {
							icon = itemIcon
							fluidIcon = ""
						}
					}
				}
				if icon == "" {
					continue
				}
				categories := fields.Categories
				if len(categories) == 0 && fields.Category != "" {
					categories = []string{fields.Category}
				}
				if len(categories) == 0 {
					categories = []string{"crafting"}
				}
				for _, category := range categories {
					iconLayers, err := readIconLayers(named[name])
					if err != nil {
						return Catalog{}, fmt.Errorf("decode icon layers for recipe %q: %w", name, err)
					}
					catalog.Prototypes = append(catalog.Prototypes, Prototype{
						Type: prototypeType, Name: name, Icon: icon, IconLayers: iconLayers, Category: category,
						FluidIcon: fluidIcon,
					})
				}
			case prototypeType == "planet" || prototypeType == "space-location" || prototypeType == "virtual-signal":
				if icon == "" {
					continue
				}
				catalog.Prototypes = append(catalog.Prototypes, Prototype{
					Type: prototypeType, Name: name, Icon: icon,
				})
			case prototypeType == "fluid":
				if icon == "" {
					continue
				}
				iconLayers, err := readIconLayers(named[name])
				if err != nil {
					return Catalog{}, fmt.Errorf("decode icon layers for fluid %q: %w", name, err)
				}
				catalog.Prototypes = append(catalog.Prototypes, Prototype{
					Type: prototypeType, Name: name, Icon: icon, IconLayers: iconLayers,
				})
			case prototypeType == "tile":
				// Most tiles have no icon of their own; the game shows the
				// item that places them.
				if icon == "" {
					icon = itemIcons[tilePlacingItem(named[name])]
				}
				if icon != "" {
					catalog.Prototypes = append(catalog.Prototypes, Prototype{Type: "tile", Name: name, Icon: icon})
				}
			case deconstructionFilterTypes[prototypeType]:
				// Entities a deconstruction planner filters on that no item
				// places: shown with their own icons.
				if icon != "" {
					catalog.Prototypes = append(catalog.Prototypes, Prototype{Type: "entity", Name: name, Icon: icon})
				}
			case isEntityPrototype(prototypeType, prototype):
				if _, exists := itemNames[name]; exists {
					catalog.Prototypes = append(catalog.Prototypes, Prototype{
						Type: "entity", Name: name, Icon: itemIcons[name],
					})
				} else if isRailEntityType(prototypeType) {
					// Rail pieces are placed by the shared "rail" item, so no
					// same-named item exists; use the entity's own icon.
					railIcon := icon
					if railIcon == "" {
						railIcon = itemIcons["rail"]
					}
					if railIcon != "" {
						catalog.Prototypes = append(catalog.Prototypes, Prototype{
							Type: "entity", Name: name, Icon: railIcon,
						})
					}
				}
			}
		}
	}
	sort.Slice(catalog.Prototypes, func(i, j int) bool {
		if catalog.Prototypes[i].Name != catalog.Prototypes[j].Name {
			return catalog.Prototypes[i].Name < catalog.Prototypes[j].Name
		}
		return catalog.Prototypes[i].Type < catalog.Prototypes[j].Type
	})
	return catalog, nil
}

// deconstructionFilterTypes are the entity types of the deconstruction
// planner's special filters (cliffs, items on the ground, ghosts, item
// request slots, tile deconstruction).
var deconstructionFilterTypes = map[string]bool{
	"cliff": true, "item-entity": true, "item-request-proxy": true,
	"entity-ghost": true, "tile-ghost": true, "deconstructible-tile-proxy": true,
}

// tilePlacingItem is the item that places a tile: placeable_by, else what
// mining it gives.
func tilePlacingItem(encoded json.RawMessage) string {
	var fields struct {
		PlaceableBy json.RawMessage `json:"placeable_by"`
		Minable     struct {
			Result string `json:"result"`
		} `json:"minable"`
	}
	if json.Unmarshal(encoded, &fields) != nil {
		return ""
	}
	type placing struct {
		Item string `json:"item"`
	}
	var one placing
	if json.Unmarshal(fields.PlaceableBy, &one) == nil && one.Item != "" {
		return one.Item
	}
	var many []placing
	if json.Unmarshal(fields.PlaceableBy, &many) == nil && len(many) > 0 && many[0].Item != "" {
		return many[0].Item
	}
	return fields.Minable.Result
}

func isRailEntityType(prototypeType string) bool {
	switch prototypeType {
	case "straight-rail", "curved-rail-a", "curved-rail-b", "half-diagonal-rail",
		"elevated-straight-rail", "elevated-curved-rail-a", "elevated-curved-rail-b", "elevated-half-diagonal-rail",
		"legacy-straight-rail", "legacy-curved-rail":
		return true
	default:
		return false
	}
}

func isEntityPrototype(prototypeType string, prototype rawPrototype) bool {
	switch prototypeType {
	case "item", "recipe", "fluid", "planet", "space-location", "virtual-signal",
		"item-group", "item-subgroup", "recipe-category", "technology", "tile",
		"achievement", "achievement-difficulty", "custom-input", "copy-paste-tool", "decorative",
		"font", "gui-style", "map-gen-presets", "map-settings", "noise-expression",
		"noise-function", "particle", "resource", "selection-tool", "sound", "sprite",
		"sticker", "trivial-smoke", "utility-constants", "utility-sounds":
		return false
	default:
		_, hasCollisionBox := prototype["collision_box"]
		_, hasSelectionBox := prototype["selection_box"]
		return hasCollisionBox || hasSelectionBox
	}
}

func readIconLayers(encoded json.RawMessage) ([]IconLayer, error) {
	var fields struct {
		Icons []json.RawMessage `json:"icons"`
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	layers := make([]IconLayer, 0, len(fields.Icons))
	for _, encodedLayer := range fields.Icons {
		icon, err := readIcon(encodedLayer)
		if err != nil {
			return nil, err
		}
		if icon == "" {
			continue
		}
		var properties struct {
			Scale float64   `json:"scale"`
			Shift []float64 `json:"shift"`
			Size  int       `json:"icon_size"`
			Tint  Color     `json:"tint"`
		}
		if err := json.Unmarshal(encodedLayer, &properties); err != nil {
			return nil, err
		}
		layers = append(layers, IconLayer{
			Icon: icon, Scale: properties.Scale, Shift: properties.Shift, Size: properties.Size,
			Tint: properties.Tint,
		})
	}
	return layers, nil
}

func hasRecyclingCategory(categories []string, category string) bool {
	if category == "recycling" {
		return true
	}
	return slices.Contains(categories, "recycling")
}

func firstItemIngredient(encoded json.RawMessage) string {
	var ingredients []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(encoded, &ingredients) == nil {
		for _, ingredient := range ingredients {
			if ingredient.Type == "" || ingredient.Type == "item" {
				return ingredient.Name
			}
		}
		return ""
	}
	var named map[string]json.RawMessage
	if json.Unmarshal(encoded, &named) != nil {
		return ""
	}
	keys := make([]string, 0, len(named))
	for key := range named {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		var ingredient struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if json.Unmarshal(named[key], &ingredient) == nil &&
			(ingredient.Type == "" || ingredient.Type == "item") && ingredient.Name != "" {
			return ingredient.Name
		}
	}
	return ""
}

type recipeResult struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func firstRecipeResult(encoded json.RawMessage) (recipeResult, error) {
	if len(encoded) == 0 {
		return recipeResult{}, nil
	}
	var results []recipeResult
	if err := json.Unmarshal(encoded, &results); err == nil {
		if len(results) > 0 {
			return results[0], nil
		}
		return recipeResult{}, nil
	}

	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &resultMap); err != nil {
		return recipeResult{}, err
	}
	names := make([]string, 0, len(resultMap))
	for name := range resultMap {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var result recipeResult
		if err := json.Unmarshal(resultMap[name], &result); err != nil {
			return recipeResult{}, err
		}
		if result.Name != "" {
			return result, nil
		}
	}
	return recipeResult{}, nil
}

func readIcon(encoded json.RawMessage) (string, error) {
	var fields struct {
		Icon  json.RawMessage   `json:"icon"`
		Icons []json.RawMessage `json:"icons"`
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return "", err
	}
	icon, err := iconPath(fields.Icon)
	if err != nil {
		return "", err
	}
	if icon != "" {
		return icon, nil
	}
	for _, layer := range fields.Icons {
		icon, err := iconPath(layer)
		if err != nil {
			return "", err
		}
		if icon != "" {
			return icon, nil
		}
	}
	return "", nil
}

func iconPath(encoded json.RawMessage) (string, error) {
	if len(encoded) == 0 || string(encoded) == "null" {
		return "", nil
	}
	var path string
	if err := json.Unmarshal(encoded, &path); err == nil {
		return path, nil
	}
	var sprite struct {
		Filename string `json:"filename"`
		Icon     string `json:"icon"`
	}
	if err := json.Unmarshal(encoded, &sprite); err != nil {
		return "", fmt.Errorf("decode icon sprite: %w", err)
	}
	if sprite.Filename != "" {
		return sprite.Filename, nil
	}
	return sprite.Icon, nil
}

func isItem(prototypeType string, prototype rawPrototype) bool {
	if prototypeType == "item" || isKnownItemType(prototypeType) {
		return true
	}
	_, hasStackSize := prototype["stack_size"]
	return hasStackSize
}

func isKnownItemType(prototypeType string) bool {
	switch prototypeType {
	case "ammo", "armor", "blueprint", "blueprint-book", "capsule",
		"deconstruction-item", "gun", "item-with-entity-data", "module",
		"repair-tool", "space-platform-starter-pack", "spidertron-remote",
		"tool", "upgrade-item", "active-defense-equipment",
		"battery-equipment", "belt-immunity-equipment", "energy-shield-equipment", "generator-equipment",
		"inventory-bonus-equipment", "movement-bonus-equipment", "night-vision-equipment",
		"roboport-equipment", "solar-panel-equipment":
		return true
	default:
		return false
	}
}
