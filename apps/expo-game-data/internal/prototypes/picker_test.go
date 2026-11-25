package prototypes

import (
	"encoding/json"
	"reflect"
	"testing"
)

func pickerDump(t *testing.T) Dump {
	t.Helper()
	raw := map[string]map[string]map[string]any{
		"item-group": {
			"logistics":             {"order": "a"},
			"intermediate-products": {"order": "c"},
			"fluids":                {"order": "f"},
			"signals":               {"order": "g"},
			"space":                 {"order": "d"},
			"other":                 {"order": "z"},
		},
		"item-subgroup": {
			"storage":         {"group": "logistics", "order": "a"},
			"belt":            {"group": "logistics", "order": "b"},
			"raw-material":    {"group": "intermediate-products", "order": "b"},
			"fluid-recipes":   {"group": "intermediate-products", "order": "a"},
			"fill-barrel":     {"group": "intermediate-products", "order": "c"},
			"fluid":           {"group": "fluids", "order": "a"},
			"virtual-signal":  {"group": "signals", "order": "a"},
			"space-platform":  {"group": "space", "order": "a"},
			"planets":         {"group": "space", "order": "j"},
			"other":           {"group": "other", "order": "a"},
			"unlisted-things": {"group": "missing", "order": "a"},
		},
		"item": {
			"iron-chest":                {"subgroup": "storage", "order": "b", "place_result": "iron-chest", "stack_size": 50},
			"wooden-chest":              {"subgroup": "storage", "order": "a", "place_result": "wooden-chest", "stack_size": 50},
			"steel-chest":               {"subgroup": "storage", "order": "b", "place_result": "steel-chest", "stack_size": 50},
			"iron-plate":                {"subgroup": "raw-material", "order": "a", "stack_size": 100},
			"tree-seed":                 {"subgroup": "raw-material", "order": "y", "place_result": "tree-plant", "stack_size": 10},
			"water-barrel":              {"subgroup": "raw-material", "order": "z", "stack_size": 10},
			"loader":                    {"subgroup": "belt", "order": "a", "place_result": "loader", "hidden": true, "stack_size": 50},
			"infinity-chest":            {"subgroup": "other", "order": "a", "place_result": "infinity-chest", "hidden": true, "stack_size": 10},
			"item-unknown":              {"hidden": true, "stack_size": 1},
			"stray":                     {"subgroup": "unlisted-things", "order": "a", "stack_size": 1},
			"space-platform-foundation": {"subgroup": "space-platform", "order": "a", "stack_size": 50},
		},
		"rail-planner": {
			"rail": {"subgroup": "belt", "order": "z", "place_result": "straight-rail", "stack_size": 100},
		},
		"container": {
			"wooden-chest":   {"collision_box": []any{}, "flags": []any{"player-creation"}},
			"iron-chest":     {"collision_box": []any{}, "flags": []any{"player-creation"}},
			"infinity-chest": {"collision_box": []any{}, "flags": []any{"player-creation"}},
		},
		"straight-rail": {"straight-rail": {"collision_box": []any{}, "flags": []any{"player-creation"}}},
		"loader-1x1":    {"loader": {"collision_box": []any{}, "flags": []any{"player-creation"}}},
		"plant":         {"tree-plant": {"collision_box": []any{}, "flags": []any{"placeable-neutral"}}},
		"planet":        {"nauvis": {"subgroup": "planets", "order": "a"}},
		"space-location": {
			"solar-system-edge":      {"subgroup": "planets", "order": "b"},
			"space-location-unknown": {"hidden": true},
		},
		"fluid": {
			"water":         {"order": "a"},
			"fluid-unknown": {"order": "z", "hidden": true},
		},
		"virtual-signal": {
			"signal-A": {"subgroup": "virtual-signal", "order": "a"},
		},
		"recipe": {
			"iron-plate":           {"results": []any{map[string]any{"type": "item", "name": "iron-plate"}}},
			"basic-oil-processing": {"subgroup": "fluid-recipes", "order": "a", "results": []any{}},
			"water-barrel":         {"subgroup": "fill-barrel", "order": "a", "results": []any{map[string]any{"type": "item", "name": "water-barrel"}}},
			"pump-water":           {"main_product": "water", "results": []any{map[string]any{"type": "fluid", "name": "water"}, map[string]any{"type": "item", "name": "iron-plate"}}},
			"no-product":           {"results": map[string]any{}},
			"iron-plate-recycling": {"hidden": true, "results": []any{map[string]any{"type": "item", "name": "iron-plate"}}},
		},
	}
	dump := Dump{}
	for prototypeType, named := range raw {
		dump[prototypeType] = map[string]json.RawMessage{}
		for name, fields := range named {
			encoded, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			dump[prototypeType][name] = encoded
		}
	}
	return dump
}

func TestExtractPicker(t *testing.T) {
	picker, err := ExtractPicker(pickerDump(t))
	if err != nil {
		t.Fatal(err)
	}
	want := Picker{
		PickerIcon: {
			{Name: "logistics", Rows: [][]string{{"[item=wooden-chest]", "[item=iron-chest]", "[item=steel-chest]"}, {"[item=rail]"}}},
			{Name: "intermediate-products", Rows: [][]string{{"[recipe=basic-oil-processing]"}, {"[item=iron-plate]", "[item=tree-seed]", "[item=water-barrel]"}}},
			{Name: "space", Rows: [][]string{{"[item=space-platform-foundation]"}, {"[planet=nauvis]", "[space-location=solar-system-edge]"}}},
			{Name: "fluids", Rows: [][]string{{"[fluid=water]"}}},
			{Name: "signals", Rows: [][]string{{"[virtual-signal=signal-A]"}}},
		},
		PickerItem: {
			{Name: "logistics", Rows: [][]string{{"[item=wooden-chest]", "[item=iron-chest]", "[item=steel-chest]"}, {"[item=rail]"}}},
			{Name: "intermediate-products", Rows: [][]string{{"[item=iron-plate]", "[item=tree-seed]", "[item=water-barrel]"}}},
			{Name: "space", Rows: [][]string{{"[item=space-platform-foundation]"}}},
		},
		PickerFluid: {
			{Name: "fluids", Rows: [][]string{{"[fluid=water]"}}},
		},
		PickerEntity: {
			{Name: "logistics", Rows: [][]string{{"[entity=wooden-chest]", "[entity=iron-chest]"}, {"[entity=straight-rail]"}}},
			{Name: "creative", Rows: [][]string{{"[entity=loader]", "[entity=infinity-chest]"}}},
		},
		PickerRecipe: {
			{Name: "intermediate-products", Rows: [][]string{{"[recipe=basic-oil-processing]"}, {"[recipe=iron-plate]"}, {"[recipe=water-barrel]"}}},
			{Name: "fluids", Rows: [][]string{{"[recipe=pump-water]"}}},
		},
	}
	for _, kind := range PickerKinds {
		if !reflect.DeepEqual(picker[kind], want[kind]) {
			t.Errorf("%s picker =\n%v\nwant\n%v", kind, picker[kind], want[kind])
		}
	}
}
