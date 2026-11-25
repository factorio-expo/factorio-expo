package prototypes

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExtractNamesAndAdded(t *testing.T) {
	base := Dump{
		"assembling-machine": {"assembling-machine-1": json.RawMessage(`{"collision_box": [[-1, -1], [1, 1]]}`)},
		"item":               {"iron-plate": json.RawMessage(`{"stack_size": 100}`)},
		"recipe": {
			"iron-gear-wheel":      json.RawMessage(`{}`),
			"iron-plate-recycling": json.RawMessage(`{"category": "recycling"}`),
		},
	}
	modded := Dump{
		"assembling-machine": {
			"assembling-machine-1": base["assembling-machine"]["assembling-machine-1"],
			"big-assembler":        json.RawMessage(`{"selection_box": [[-2, -2], [2, 2]]}`),
		},
		"explosion": {"no-box": json.RawMessage(`{}`)},
		"tile":      {"concrete": json.RawMessage(`{"collision_box": [[0, 0], [1, 1]]}`)},
		"recipe": {
			"iron-gear-wheel":         json.RawMessage(`{}`),
			"big-assembler":           json.RawMessage(`{"categories": ["crafting"]}`),
			"big-assembler-recycling": json.RawMessage(`{"categories": ["recycling"]}`),
		},
	}
	baseNames, err := ExtractNames(base)
	if err != nil {
		t.Fatal(err)
	}
	moddedNames, err := ExtractNames(modded)
	if err != nil {
		t.Fatal(err)
	}
	entities, recipes := moddedNames.Added(baseNames)
	if !reflect.DeepEqual(entities, []string{"big-assembler"}) || !reflect.DeepEqual(recipes, []string{"big-assembler"}) {
		t.Fatalf("added = %v / %v", entities, recipes)
	}
	entities, recipes = baseNames.Added(Names{})
	if !reflect.DeepEqual(entities, []string{"assembling-machine-1"}) || !reflect.DeepEqual(recipes, []string{"iron-gear-wheel"}) {
		t.Fatalf("base = %v / %v", entities, recipes)
	}
}
