package search

import "testing"

func TestBuildFilter(t *testing.T) {
	query := Query{
		Icons:         []string{"[fluid=water]"},
		Tags:          []string{"logistics", "smelting"},
		Mods:          []string{"vanilla", "space-age"},
		Entities:      []string{"assembling-machine-2"},
		GameVersion:   "2.1.0",
		PlatformsOnly: true,
		PlatformFilters: []PlatformFilter{
			{Metric: "weight_tons", Operator: ">", Value: 25},
			{Metric: "max_speed_km_s", Operator: "=", Value: 100},
		},
		ProductionFilters: []ProductionFilter{
			{
				Item:      "iron-gear-wheel",
				Direction: "out",
				Operator:  ">",
				Rate:      5,
			},
			{
				Item:      "iron-plate",
				Quality:   "rare",
				Direction: "out",
				Operator:  ">",
				Rate:      2,
			},
		},
	}

	got := buildFilter(query)
	want := "icons:=`[fluid=water]` && tags:=[`logistics`,`smelting`] && mods:=[`vanilla`,`space-age`] && " +
		"entity_names:=[`assembling-machine-2`] && game_version:=`2.1.0` && " +
		"has_platform:=true && weight_tons:>25 && max_speed_km_s:=100 && " +
		"rate_out_iron_gear_wheel:>5 && rate_out_rare_iron_plate:>2"
	if got != want {
		t.Errorf("buildFilter =\n%q\nwant\n%q", got, want)
	}
}

func TestBuildPlatformFilterRequiresAPlatformWhenOnlySelected(t *testing.T) {
	if got := buildFilter(Query{PlatformsOnly: true}); got != "has_platform:=true" {
		t.Fatalf("platform-only filter = %q, want has_platform:=true", got)
	}
}

func TestBuildFactoryFilterExcludesPlatformEntries(t *testing.T) {
	if got := buildFilter(Query{FactoriesOnly: true}); got != "has_platform:=false" {
		t.Fatalf("factory-only filter = %q, want has_platform:=false", got)
	}
}

func TestBuildFilterEmpty(t *testing.T) {
	if got := buildFilter(Query{}); got != "" {
		t.Errorf("buildFilter(empty) = %q, want empty", got)
	}
}

func TestBuildFilterSeparatesEntityAndRecipeQuality(t *testing.T) {
	got := buildFilter(Query{
		Entities:      []string{"biolab"},
		EntityQuality: "legendary",
		Recipes:       []string{"automation-science-pack"},
		RecipeQuality: "rare",
	})
	want := "entity_facts:=[`legendary:biolab`] && recipe_facts:=[`rare:automation-science-pack`]"
	if got != want {
		t.Errorf("buildFilter = %q, want %q", got, want)
	}
}

func TestBuildProductionFiltersWithComparisons(t *testing.T) {
	query := Query{
		ProductionFilters: []ProductionFilter{
			{Item: "iron-plate", Direction: "out", Operator: ">", Rate: 5},
			{Item: "copper-plate", Direction: "in", Operator: "=", Rate: 2.5},
			{Item: "water", Direction: "in", Operator: "<", Rate: 10},
		},
	}
	got := buildFilter(query)
	want := "rate_out_iron_plate:>5 && rate_in_copper_plate:=2.5 && rate_in_water:<10"
	if got != want {
		t.Errorf("buildFilter = %q, want %q", got, want)
	}
}

func TestBuildPerTileAndPerMWAndCostFilters(t *testing.T) {
	query := Query{
		ProductionFilters: []ProductionFilter{
			{Item: "iron-plate", Direction: DirectionOutPerTile, Operator: ">", Rate: 0.5},
			{Item: "electronic-circuit", Quality: "rare", Direction: DirectionOutPerMW, Operator: "<", Rate: 2},
		},
		CostFilters: []CostFilter{
			{Resource: "iron-ore", Operator: "<", Amount: 100},
			{Resource: "crude-oil", Operator: "=", Amount: 0},
		},
		Spoiler: SpoilerExclude,
	}
	got := buildFilter(query)
	want := "rate_tile_out_iron_plate:>0.5 && rate_mw_out_rare_electronic_circuit:<2 && " +
		"cost_raw_iron_ore:<100 && cost_raw_crude_oil:=0 && spoiler:=false"
	if got != want {
		t.Errorf("buildFilter = %q, want %q", got, want)
	}
	if got := buildFilter(Query{Spoiler: SpoilerOnly}); got != "spoiler:=true" {
		t.Errorf("spoiler only = %q", got)
	}
}

func TestBuildGameVersionFilterIgnoresPatchVersion(t *testing.T) {
	tests := map[string]string{
		"pre-2.0":         "version_groups:=`pre-2.0`",
		"v2.0":            "version_groups:=`v2.0`",
		"v2.1":            "version_groups:=`v2.1`",
		"v2.1-compatible": "version_groups:=`v2.1-compatible`",
		"2.1.7":           "game_version:=`2.1.7`",
	}
	for version, want := range tests {
		if got := buildGameVersionFilter(version); got != want {
			t.Errorf("buildGameVersionFilter(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestBuildFilterRootKindAndFootprint(t *testing.T) {
	tests := map[string]struct {
		query Query
		want  string
	}{
		"kinds": {
			Query{Kinds: []string{"book", "upgrade_planner"}},
			"kind:=[`book`,`upgrade_planner`]",
		},
		// Either way round: the smaller bound limits the short side.
		"both sides": {
			Query{FootprintWidth: 40, FootprintHeight: 20},
			"footprint_short:<=20 && footprint_long:<=40",
		},
		"one side": {Query{FootprintHeight: 30}, "footprint_short:<=30"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := buildFilter(test.query); got != test.want {
				t.Fatalf("buildFilter = %q, want %q", got, test.want)
			}
		})
	}
}
