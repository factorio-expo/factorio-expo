package prototypes

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Names are the entity and recipe names a dump defines: what a blueprint can
// reference as an entity's name or an assembler's recipe.
type Names struct {
	Entities map[string]struct{}
	Recipes  map[string]struct{}
}

// ExtractNames lists the dump's entities (prototypes with a collision or
// selection box, as the icon sheets decide it) and its recipes, leaving out
// recycling recipes: no blueprint can set one.
func ExtractNames(dump Dump) (Names, error) {
	names := Names{Entities: map[string]struct{}{}, Recipes: map[string]struct{}{}}
	for prototypeType, named := range dump {
		for name, encoded := range named {
			var prototype rawPrototype
			if err := json.Unmarshal(encoded, &prototype); err != nil {
				return Names{}, fmt.Errorf("decode %s prototype %q: %w", prototypeType, name, err)
			}
			if prototypeType == "recipe" {
				var fields struct {
					Category   string   `json:"category"`
					Categories []string `json:"categories"`
				}
				if err := json.Unmarshal(encoded, &fields); err != nil {
					return Names{}, fmt.Errorf("decode recipe %q: %w", name, err)
				}
				if !hasRecyclingCategory(fields.Categories, fields.Category) {
					names.Recipes[name] = struct{}{}
				}
				continue
			}
			if isEntityPrototype(prototypeType, prototype) {
				names.Entities[name] = struct{}{}
			}
		}
	}
	return names, nil
}

// Added returns the names in names that parent lacks, sorted.
func (names Names) Added(parent Names) (entities, recipes []string) {
	return missing(names.Entities, parent.Entities), missing(names.Recipes, parent.Recipes)
}

func missing(set, parent map[string]struct{}) []string {
	out := []string{}
	for name := range set {
		if _, ok := parent[name]; !ok {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}
