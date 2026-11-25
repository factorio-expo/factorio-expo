package prototypes

import (
	"encoding/json"
	"testing"
)

func TestExtractsSupportedPrototypeCategoriesAndGroups(t *testing.T) {
	dump := map[string]map[string]json.RawMessage{
		"item": {
			"offshore-pump":    json.RawMessage(`{"icon":"__base__/graphics/icons/offshore-pump-item.png","stack_size":50,"subgroup":"production"}`),
			"advanced-circuit": json.RawMessage(`{"icon":"__base__/graphics/icons/advanced-circuit.png","stack_size":200}`),
			"steam-turbine":    json.RawMessage(`{"icon":"__base__/graphics/icons/steam-turbine.png","stack_size":50}`),
		},
		"assembling-machine": {
			"offshore-pump": json.RawMessage(`{"icon":"__base__/graphics/icons/offshore-pump-entity.png","selection_box":[[0,0],[1,1]]}`),
		},
		"generator": {
			"steam-turbine": json.RawMessage(`{"selection_box":[[0,0],[1,1]]}`),
		},
		"simple-entity": {
			"icon-only-entity": json.RawMessage(`{"icon":"__base__/graphics/icons/icon-only-entity.png"}`),
		},
		"recipe": {
			"offshore-pump":       json.RawMessage(`{"result":"offshore-pump","categories":["crafting","smelting"]}`),
			"advanced-circuit":    json.RawMessage(`{"result":"advanced-circuit","category":"crafting"}`),
			"recipe-with-no-icon": json.RawMessage(`{"results":{}}`),
			"lubricant":           json.RawMessage(`{"result":"lubricant","category":"chemistry"}`),
		},
		"fluid": {
			"water":     json.RawMessage(`{"icon":"__base__/graphics/icons/fluid/water.png"}`),
			"lubricant": json.RawMessage(`{"icon":"__base__/graphics/icons/fluid/lubricant.png"}`),
		},
		"planet": {
			"nauvis": json.RawMessage(`{"icon":{"filename":"__base__/graphics/icons/nauvis.png","size":64}}`),
		},
		"space-location": {
			"nauvis-orbit": json.RawMessage(`{"icons":[{"icon":"__base__/graphics/icons/nauvis-orbit.png"}]}`),
		},
		"virtual-signal": {
			"signal-A": json.RawMessage(`{"icon":"__base__/graphics/icons/signal-A.png"}`),
		},
		"item-subgroup": {
			"production": json.RawMessage(`{"group":"production-group"}`),
		},
	}

	catalog, err := Extract(dump)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Prototypes) != 14 {
		t.Fatalf("prototype count = %d, want 14: %+v", len(catalog.Prototypes), catalog.Prototypes)
	}
	assertPrototype(t, catalog, "item", "offshore-pump", "production-group", "")
	assertPrototype(t, catalog, "entity", "offshore-pump", "", "")
	assertPrototype(t, catalog, "entity", "steam-turbine", "", "")
	assertPrototype(t, catalog, "item", "advanced-circuit", "ungrouped", "")
	assertPrototype(t, catalog, "recipe", "advanced-circuit", "", "crafting")
	assertPrototype(t, catalog, "item", "steam-turbine", "ungrouped", "")
	assertPrototype(t, catalog, "recipe", "offshore-pump", "", "crafting")
	assertPrototype(t, catalog, "recipe", "offshore-pump", "", "smelting")
	assertPrototype(t, catalog, "planet", "nauvis", "", "")
	assertPrototype(t, catalog, "space-location", "nauvis-orbit", "", "")
	assertPrototype(t, catalog, "virtual-signal", "signal-A", "", "")
	assertPrototype(t, catalog, "fluid", "water", "", "")
	assertPrototype(t, catalog, "fluid", "lubricant", "", "")
	assertPrototype(t, catalog, "recipe", "lubricant", "", "chemistry")
	for _, prototype := range catalog.Prototypes {
		if prototype.Type == "assembling-machine" || prototype.Type == "simple-entity" {
			t.Fatalf("entity prototype %q should be excluded", prototype.Name)
		}
		if prototype.Type == "entity" && prototype.Name == "advanced-circuit" {
			t.Fatal("advanced-circuit should not be classified as an entity")
		}
		if prototype.Type == "recipe" && prototype.Name == "offshore-pump" {
			if prototype.Icon != "__base__/graphics/icons/offshore-pump-item.png" {
				t.Fatalf("recipe icon = %q, want result item's icon", prototype.Icon)
			}
		}
	}
}

func TestExtractsRecyclingRecipeItemIconLayers(t *testing.T) {
	dump := map[string]map[string]json.RawMessage{
		"item": {
			"iron-plate": json.RawMessage(`{"icon":"__base__/graphics/icons/iron-plate.png","stack_size":100}`),
		},
		"recipe": {
			"iron-plate-recycling": json.RawMessage(`{
				"categories":["recycling"],
				"ingredients":[{"type":"item","name":"iron-plate","amount":1}],
				"icons":[
					{"icon":"__recycler__/graphics/icons/recycling.png","tint":[0,0.34,0.6,0.75]},
					{"icon":"__base__/graphics/icons/iron-plate.png","icon_size":64,"scale":0.4},
					{"icon":"__recycler__/graphics/icons/recycling-top.png"}
				]
			}`),
		},
	}

	catalog, err := Extract(dump)
	if err != nil {
		t.Fatal(err)
	}
	for _, prototype := range catalog.Prototypes {
		if prototype.Type != "recipe" || prototype.Name != "iron-plate-recycling" {
			continue
		}
		if prototype.Icon != "__base__/graphics/icons/iron-plate.png" {
			t.Fatalf("recycling icon = %q, want the ingredient item icon", prototype.Icon)
		}
		if len(prototype.IconLayers) != 3 || prototype.IconLayers[1].Scale != 0.4 {
			t.Fatalf("recycling icon layers = %+v, want all three upstream-generated layers", prototype.IconLayers)
		}
		if prototype.IconLayers[0].Tint != (Color{Red: 0, Green: 0.34, Blue: 0.6, Alpha: 0.75, Set: true}) {
			t.Fatalf("recycling tint = %+v, want array-form tint values", prototype.IconLayers[0].Tint)
		}
		return
	}
	t.Fatal("recycling recipe was not included")
}

func TestExtractsFluidRecipeIconFallbacks(t *testing.T) {
	for _, results := range []string{
		`"results":[{"type":"fluid","name":"water","amount":10}]`,
		`"results":{"1":{"type":"fluid","name":"water","amount":10}}`,
		`"result":"water"`,
	} {
		t.Run(results, func(t *testing.T) {
			catalog, err := Extract(map[string]map[string]json.RawMessage{
				"fluid": {"water": json.RawMessage(`{"icon":"__base__/water.png"}`)},
				"recipe": {
					"make-water": json.RawMessage(`{` + results + `}`),
					"explicit":   json.RawMessage(`{"icon":"__base__/recipe.png",` + results + `}`),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			assertPrototype(t, catalog, "recipe", "make-water", "", "crafting")
			for _, prototype := range catalog.Prototypes {
				if prototype.Name == "make-water" && (prototype.FluidIcon != "water" || prototype.Icon != "__base__/water.png") {
					t.Fatalf("fluid fallback = %+v", prototype)
				}
				if prototype.Name == "explicit" && (prototype.FluidIcon != "" || prototype.Icon != "__base__/recipe.png") {
					t.Fatalf("explicit recipe icon should not be replaced: %+v", prototype)
				}
			}
		})
	}
}

func assertPrototype(t *testing.T, catalog Catalog, prototypeType, name, group, category string) {
	t.Helper()
	for _, prototype := range catalog.Prototypes {
		if prototype.Type != prototypeType || prototype.Name != name {
			continue
		}
		if prototype.Group == group && prototype.Category == category && prototype.Icon != "" {
			return
		}
	}
	t.Fatalf("missing %s prototype %q with group %q, category %q, and an icon", prototypeType, name, group, category)
}

func TestExtractsRailEntitiesWithoutSameNamedItem(t *testing.T) {
	dump := map[string]map[string]json.RawMessage{
		"rail-planner": {
			"rail": json.RawMessage(`{"icon":"__base__/graphics/icons/rail.png","stack_size":100}`),
		},
		"straight-rail": {
			"straight-rail": json.RawMessage(`{"icon":"__base__/graphics/icons/rail-entity.png","collision_box":[[0,0],[1,1]]}`),
		},
		"curved-rail-a": {
			"curved-rail-a": json.RawMessage(`{"collision_box":[[0,0],[1,1]]}`),
		},
		"legacy-curved-rail": {
			"legacy-curved-rail": json.RawMessage(`{"collision_box":[[0,0],[1,1]]}`),
		},
	}
	catalog, err := Extract(dump)
	if err != nil {
		t.Fatal(err)
	}
	icons := map[string]string{}
	for _, prototype := range catalog.Prototypes {
		if prototype.Type == "entity" {
			icons[prototype.Name] = prototype.Icon
		}
	}
	want := map[string]string{
		"straight-rail":      "__base__/graphics/icons/rail-entity.png",
		"curved-rail-a":      "__base__/graphics/icons/rail.png",
		"legacy-curved-rail": "__base__/graphics/icons/rail.png",
	}
	for name, icon := range want {
		if icons[name] != icon {
			t.Fatalf("entity %q icon = %q, want %q", name, icons[name], icon)
		}
	}
}

func TestExtractsTilesAndDeconstructionFilterEntities(t *testing.T) {
	dump := map[string]map[string]json.RawMessage{
		"item": {
			"stone-brick":             json.RawMessage(`{"icon":"__base__/graphics/icons/stone-brick.png","stack_size":100}`),
			"refined-hazard-concrete": json.RawMessage(`{"icon":"__base__/graphics/icons/refined-hazard-concrete.png","stack_size":100}`),
		},
		"tile": {
			"stone-path":                   json.RawMessage(`{"minable":{"result":"stone-brick"}}`),
			"refined-hazard-concrete-left": json.RawMessage(`{"minable":{"result":"refined-hazard-concrete"},"placeable_by":{"item":"refined-hazard-concrete","count":1}}`),
			"water":                        json.RawMessage(`{}`),
			"own-icon":                     json.RawMessage(`{"icon":"__base__/graphics/icons/own-icon.png"}`),
		},
		"cliff":        {"cliff": json.RawMessage(`{"icon":"__base__/graphics/icons/cliff.png"}`)},
		"entity-ghost": {"entity-ghost": json.RawMessage(`{"icon":"__core__/graphics/icons/mip/ghost-entity.png"}`)},
		"item-entity":  {"item-on-ground": json.RawMessage(`{"icon":"__core__/graphics/item-on-ground.png"}`)},
	}
	catalog, err := Extract(dump)
	if err != nil {
		t.Fatal(err)
	}
	icons := map[string]string{}
	for _, prototype := range catalog.Prototypes {
		icons[prototype.Type+"/"+prototype.Name] = prototype.Icon
	}
	want := map[string]string{
		"tile/stone-path":                   "__base__/graphics/icons/stone-brick.png",
		"tile/refined-hazard-concrete-left": "__base__/graphics/icons/refined-hazard-concrete.png",
		"tile/own-icon":                     "__base__/graphics/icons/own-icon.png",
		"entity/cliff":                      "__base__/graphics/icons/cliff.png",
		"entity/entity-ghost":               "__core__/graphics/icons/mip/ghost-entity.png",
		"entity/item-on-ground":             "__core__/graphics/item-on-ground.png",
	}
	for key, icon := range want {
		if icons[key] != icon {
			t.Errorf("%s icon = %q, want %q", key, icons[key], icon)
		}
	}
	if _, ok := icons["tile/water"]; ok {
		t.Error("a tile no item places has no icon")
	}
}
