// Package modtable is the wire shape of the mod name table: which entity and
// recipe names each modset adds. expo-game-data writes it; expo-api embeds it
// to detect the mods a blueprint needs (docs/architecture.md#mod-detection).
package modtable

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// Table lists every modset, built-in ones (the game's own mods: base,
// elevated-rails, quality, recycler, space-age) first.
type Table struct {
	// FactorioVersion is the pinned game version the dumps ran on.
	FactorioVersion string   `json:"factorio_version"`
	Modsets         []Modset `json:"modsets"`
}

// Modset is one detectable unit: a built-in mod, or a curated list of portal
// mods together with their dependencies.
type Modset struct {
	// Name is the modset's id (a built-in's mod name).
	Name  string `json:"name"`
	Title string `json:"title"`
	// Builtin marks the game's own mods; a name they add is an exact match.
	Builtin bool `json:"builtin"`
	// Mods are the mods loaded for this modset's dump, dependencies included.
	Mods []Mod `json:"mods"`
	// Entities and Recipes are the names this modset adds on top of the
	// built-in mods it depends on (base's lists are the whole base game).
	Entities []string `json:"entities"`
	Recipes  []string `json:"recipes"`
}

// Mod is one loaded mod and the version used.
type Mod struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Parse decodes and checks a table.
func Parse(data []byte) (Table, error) {
	var table Table
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&table); err != nil {
		return Table{}, fmt.Errorf("decode mod table: %w", err)
	}
	seen := map[string]bool{}
	for _, modset := range table.Modsets {
		if modset.Name == "" {
			return Table{}, fmt.Errorf("mod table: modset without a name")
		}
		if seen[modset.Name] {
			return Table{}, fmt.Errorf("mod table: duplicate modset %q", modset.Name)
		}
		seen[modset.Name] = true
	}
	return table, nil
}

// Marshal encodes a table deterministically (names sorted, one per line), so
// an unchanged game and mod list produce identical bytes.
func Marshal(table Table) ([]byte, error) {
	for i := range table.Modsets {
		modset := &table.Modsets[i]
		modset.Entities = sorted(modset.Entities)
		modset.Recipes = sorted(modset.Recipes)
		if modset.Mods == nil {
			modset.Mods = []Mod{}
		}
	}
	data, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode mod table: %w", err)
	}
	return append(data, '\n'), nil
}

func sorted(names []string) []string {
	if names == nil {
		return []string{}
	}
	names = slices.Clone(names)
	slices.Sort(names)
	return slices.Compact(names)
}
