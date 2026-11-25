package search

import (
	"factorio-expo/internal/analysisresult"
	"reflect"
	"testing"
)

func TestDecodeQualitiesCollectsStructuralAndProductionTiers(t *testing.T) {
	entities := []byte(`[{"name":"biolab","quality":"normal","count":2},{"name":"biolab","quality":"legendary","count":1}]`)
	recipes := []byte(`[{"name":"automation-science-pack","quality":"rare"}]`)
	production := []string{"epic", "normal"}

	qualities, err := decodeQualities(entities, recipes, production)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"epic", "legendary", "normal", "rare"}; !reflect.DeepEqual(qualities, want) {
		t.Fatalf("qualities = %v, want %v", qualities, want)
	}
}

func TestDecodeRecipesDeduplicatesNamesAcrossQualities(t *testing.T) {
	raw := []byte(`[{"name":"automation-science-pack","quality":"normal"},{"name":"automation-science-pack","quality":"rare"}]`)
	recipes, err := decodeRecipes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"automation-science-pack"}; !reflect.DeepEqual(recipes, want) {
		t.Fatalf("recipes = %v, want %v", recipes, want)
	}
}

func TestDecodeStructuralQualityFacets(t *testing.T) {
	entities, err := decodeEntityFacts([]byte(`[{"name":"biolab","quality":"rare"},{"name":"biolab","quality":"normal"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"normal:biolab", "rare:biolab"}; !reflect.DeepEqual(entities, want) {
		t.Fatalf("entity facts = %v, want %v", entities, want)
	}

	recipes, err := decodeRecipeFacts([]byte(`[{"name":"automation-science-pack","quality":"legendary"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"legendary:automation-science-pack"}; !reflect.DeepEqual(recipes, want) {
		t.Fatalf("recipe facts = %v, want %v", recipes, want)
	}
}

func TestSnapshotFactsQualityRates(t *testing.T) {
	raw := []byte(`{"products":[{"type":"item","name":"iron-plate","quality":"rare","output_per_second":2}],"ingredients":[],"intermediates":[],"electricity":{"net_watts":-5},"pollution":{"net_per_second":1}}`)
	snapshot, err := analysisresult.ParseSnapshot(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	facts := snapshot.Facts()
	fields := buildRateFields(facts.OutRates, facts.InRates, facts.QualityOutRates, facts.QualityInRates, 0, 0, nil)
	if fields["rate_out_iron_plate"] != 2 || fields["rate_out_rare_iron_plate"] != 2 || len(fields) != 2 {
		t.Fatalf("rate fields = %v, want aggregate and rare rates only", fields)
	}
	if facts.ElectricityNetWatts != -5 || facts.PollutionNetPerSecond != 1 {
		t.Fatalf("facts = %+v", facts)
	}
}

// A leaf's footprint and consumption add its output rates per tile and per MW
// (never its input rates), and its build cost one field per raw resource.
func TestBuildRateFieldsPerTileAndPerMW(t *testing.T) {
	raw := []byte(`{"products":[{"type":"item","name":"iron-plate","quality":"rare","output_per_second":2}],` +
		`"ingredients":[{"type":"item","name":"iron-ore","quality":"normal","input_per_second":4}],"intermediates":[],` +
		`"electricity":{"consumption_watts":500000,"production_watts":0,"net_watts":-500000},"pollution":{"net_per_second":1},` +
		`"footprint":{"width":4,"height":5},` +
		`"build_cost":{"items":[],"raw":[{"type":"item","name":"iron-ore","amount":30},{"type":"fluid","name":"water","amount":2.5}]}}`)
	snapshot, err := analysisresult.ParseSnapshot(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	facts := snapshot.Facts()
	fields := buildRateFields(facts.OutRates, facts.InRates, facts.QualityOutRates, facts.QualityInRates,
		facts.FootprintTiles(), facts.ConsumptionMW(), facts.RawCost)
	want := map[string]float64{
		"rate_out_iron_plate": 2, "rate_out_rare_iron_plate": 2, "rate_in_iron_ore": 4, "rate_in_normal_iron_ore": 4,
		"rate_tile_out_iron_plate": 0.1, "rate_tile_out_rare_iron_plate": 0.1,
		"rate_mw_out_iron_plate": 4, "rate_mw_out_rare_iron_plate": 4,
		"cost_raw_iron_ore": 30, "cost_raw_water": 2.5,
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
}

// No consumption, no per-MW fields; no footprint, no per-tile fields.
func TestBuildRateFieldsWithoutFootprintOrConsumption(t *testing.T) {
	fields := buildRateFields(map[string]float64{"steam": 60}, nil, nil, nil, 0, 0, nil)
	if want := map[string]float64{"rate_out_steam": 60}; !reflect.DeepEqual(fields, want) {
		t.Fatalf("fields = %v, want %v", fields, want)
	}
}
