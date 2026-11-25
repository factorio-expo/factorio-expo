package analysisresult

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const goldenJob = "pj_golden"

func golden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "test-data", "analysis", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mutate decodes the golden success fixture, applies change, and re-encodes it.
func mutate(t *testing.T, change func(map[string]any)) []byte {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(golden(t, "analysis-succeeded.json"), &body); err != nil {
		t.Fatal(err)
	}
	change(body)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseGoldenSucceeded(t *testing.T) {
	result, failure, err := Parse(golden(t, "analysis-succeeded.json"), goldenJob)
	if err != nil || failure != nil || result == nil {
		t.Fatalf("Parse() = %v, %v, %v", result, failure, err)
	}
	if result.State != StateComplete {
		t.Fatalf("state = %q", result.State)
	}
	// The precision case: 320/3 consumed, 106.5 produced.
	gear := result.Snapshot.Intermediates[0]
	if gear.InputPerSecond != 320.0/3 || gear.OutputPerSecond != 106.5 {
		t.Fatalf("gear rates = %v / %v", gear.InputPerSecond, gear.OutputPerSecond)
	}
	if footprint := result.Snapshot.Footprint; footprint == nil || footprint.Width != 12 || footprint.Height != 7 {
		t.Fatalf("footprint = %+v", footprint)
	}
	cost := result.Snapshot.BuildCost
	if cost == nil || len(cost.Items) != 1 || cost.Items[0].Count != 2 || cost.Items[0].Quality != "rare" ||
		len(cost.Raw) != 2 || cost.Raw[0].Name != "iron-ore" || cost.Raw[1].Type != "fluid" {
		t.Fatalf("build cost = %+v", cost)
	}
	stored, err := result.Stored()
	if err != nil {
		t.Fatal(err)
	}
	if stored.SchemaVersion != SchemaVersion || stored.State != StateComplete {
		t.Fatalf("stored = %+v", stored)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(stored.Snapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"job_id", "status", "phase", "tick", "state", "schema_version"} {
		if _, ok := snapshot[key]; ok {
			t.Fatalf("transport field %q leaked into snapshot: %s", key, stored.Snapshot)
		}
	}
	for _, key := range snapshotKeys {
		if _, ok := snapshot[key]; !ok {
			t.Fatalf("snapshot is missing %q", key)
		}
	}
}

func TestParseGoldenPlatformAndFailure(t *testing.T) {
	result, _, err := Parse(golden(t, "analysis-platform.json"), goldenJob)
	if err != nil {
		t.Fatal(err)
	}
	platform := result.Snapshot.Facts().Platform
	if result.State != StatePartial || platform == nil || *platform.MaxSpeedKMS != 230.25 {
		t.Fatalf("state %q platform %+v", result.State, platform)
	}
	result, failure, err := Parse(golden(t, "analysis-failed.json"), goldenJob)
	if err != nil || result != nil || failure == nil || failure.Code != "analysis_failed" || failure.Message != "boom" {
		t.Fatalf("Parse() = %v, %v, %v", result, failure, err)
	}
}

// Regression: the bridge used to write 14 significant digits, so the
// recomputed output-input differed from difference_per_second by more than the
// old absolute-ish tolerance, and valid analyses were rejected as invalid_result.
func TestParseAcceptsRoundedDifference(t *testing.T) {
	data := mutate(t, func(body map[string]any) {
		row := body["intermediates"].([]any)[0].(map[string]any)
		row["input_per_second"] = 106.66666666667 // %.14g of 320/3
		row["output_per_second"] = 106.5
		row["difference_per_second"] = -0.16666666666667 // %.14g of the full-precision difference
	})
	if _, _, err := Parse(data, goldenJob); err != nil {
		t.Fatalf("rounded 14-digit result rejected: %v", err)
	}
}

func TestParseRejectsInvalid(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]any)
	}{
		{"schema version", func(b map[string]any) { b["schema_version"] = 2 }},
		{"status", func(b map[string]any) { b["status"] = "pending" }},
		{"phase", func(b map[string]any) { b["phase"] = "preview" }},
		{"other job", func(b map[string]any) { b["job_id"] = "other" }},
		{"missing tick", func(b map[string]any) { delete(b, "tick") }},
		{"state", func(b map[string]any) { b["state"] = "great" }},
		{"missing platform", func(b map[string]any) { delete(b, "space_platform") }},
		{"platform array", func(b map[string]any) { b["space_platform"] = []any{} }},
		{"platform without metrics", func(b map[string]any) { b["space_platform"] = map[string]any{"is_space_platform": true} }},
		{"non-platform metrics", func(b map[string]any) {
			b["space_platform"] = map[string]any{"is_space_platform": false, "weight_tons": 1}
		}},
		{"null array", func(b map[string]any) { b["ingredients"] = nil }},
		{"material type", func(b map[string]any) { b["products"].([]any)[0].(map[string]any)["type"] = "thing" }},
		{"item without quality", func(b map[string]any) { b["products"].([]any)[0].(map[string]any)["quality"] = "" }},
		{"fluid with quality", func(b map[string]any) { b["ingredients"].([]any)[0].(map[string]any)["quality"] = "normal" }},
		{"negative rate", func(b map[string]any) { b["products"].([]any)[0].(map[string]any)["output_per_second"] = -1 }},
		{"wrong difference", func(b map[string]any) {
			b["intermediates"].([]any)[0].(map[string]any)["difference_per_second"] = 5
		}},
		{"electricity", func(b map[string]any) { delete(b["electricity"].(map[string]any), "net_watts") }},
		{"pollution", func(b map[string]any) { b["pollution"].(map[string]any)["production_per_second"] = -1 }},
		{"calculator", func(b map[string]any) { b["calculator"] = []any{} }},
		{"missing footprint", func(b map[string]any) { delete(b, "footprint") }},
		{"empty footprint", func(b map[string]any) { b["footprint"] = map[string]any{"width": 0, "height": 3} }},
		{"missing build cost", func(b map[string]any) { delete(b, "build_cost") }},
		{"build item count", func(b map[string]any) {
			b["build_cost"].(map[string]any)["items"].([]any)[0].(map[string]any)["count"] = 1.5
		}},
		{"raw amount", func(b map[string]any) {
			b["build_cost"].(map[string]any)["raw"].([]any)[0].(map[string]any)["amount"] = 0
		}},
		{"raw type", func(b map[string]any) {
			b["build_cost"].(map[string]any)["raw"].([]any)[0].(map[string]any)["type"] = "energy"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := Parse(mutate(t, test.change), goldenJob); err == nil {
				t.Fatal("expected invalid analysis result")
			}
		})
	}
}

func TestParseRejectsTrailingValueAndOversize(t *testing.T) {
	data := append(golden(t, "analysis-succeeded.json"), []byte(" {}")...)
	if _, _, err := Parse(data, goldenJob); err == nil {
		t.Fatal("trailing JSON value accepted")
	}
	if _, _, err := Parse(make([]byte, MaxBytes+1), goldenJob); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize err = %v", err)
	}
	if _, err := ReadAll(strings.NewReader(strings.Repeat("x", MaxBytes+1))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("ReadAll oversize err = %v", err)
	}
}

func TestStoredSnapshotRoundTripsAndDerives(t *testing.T) {
	result, _, err := Parse(golden(t, "analysis-succeeded.json"), goldenJob)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := result.Stored()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseSnapshot(stored.Snapshot, stored.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	view, err := snapshot.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.SpacePlatform == nil || view.SpacePlatform.IsSpacePlatform ||
		!slices.ContainsFunc(view.Ingredients, func(m MaterialRate) bool { return m.Name == "water" && m.Quality == nil }) ||
		view.Calculator == nil || view.Calculator.ContentSet != "space-age" ||
		view.Electricity == nil || view.Electricity.ConsumptionWatts != 90000 ||
		view.Pollution == nil || view.Pollution.Pollutant == nil || *view.Pollution.Pollutant != "pollution" ||
		view.Completeness == nil || view.Completeness.Materials.Status != "complete" {
		t.Fatalf("view = %+v", view)
	}
	facts := snapshot.Facts()
	if facts.OutRates["electronic-circuit"] != 0.5 || facts.OutRates["iron-gear-wheel"] != 106.5 ||
		facts.InRates["iron-gear-wheel"] != 320.0/3 || facts.InRates["water"] != 12.5 {
		t.Fatalf("rates out=%v in=%v", facts.OutRates, facts.InRates)
	}
	if facts.QualityOutRates[QualityKey("rare", "electronic-circuit")] != 0.5 {
		t.Fatalf("quality rates out=%v in=%v", facts.QualityOutRates, facts.QualityInRates)
	}
	// Fluids have no quality: water has an aggregate rate and no quality rate.
	for key := range facts.QualityInRates {
		if quality, name, _ := strings.Cut(key, "\x00"); quality == "" || name == "water" {
			t.Fatalf("quality in rates carry a fluid: %q", key)
		}
	}
	if got := strings.Join(facts.Qualities, ","); got != "normal,rare" {
		t.Fatalf("qualities = %q", got)
	}
	if facts.ElectricityNetWatts != -90000 || facts.PollutionNetPerSecond != 1.5 || facts.Platform != nil {
		t.Fatalf("facts = %+v", facts)
	}
	if facts.ElectricityConsumptionWatts != 90000 || facts.ElectricityProductionWatts != 0 || facts.ConsumptionMW() != 0.09 {
		t.Fatalf("electricity = %+v", facts)
	}
	if facts.FootprintTiles() != 84 || facts.RawCost["iron-ore"] != 47.5 || facts.RawCost["water"] != 1.0/3 {
		t.Fatalf("footprint tiles = %v, raw cost = %v", facts.FootprintTiles(), facts.RawCost)
	}
	// A fluid row is identified by its name: the golden carries no temperature.
	if bytes.Contains(stored.Snapshot, []byte("temperature")) {
		t.Fatalf("snapshot carries a temperature: %s", stored.Snapshot)
	}
}

func TestViewCarriesUnresolvedCauses(t *testing.T) {
	result, _, err := Parse(golden(t, "analysis-platform.json"), goldenJob)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := result.Stored()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ParseSnapshot(stored.Snapshot, stored.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	view, err := snapshot.View()
	if err != nil {
		t.Fatal(err)
	}
	want := []Unresolved{{Cause: "unavailable_prototype", Entity: "modded-furnace", Count: 1}}
	if view.Completeness == nil || !slices.Equal(view.Completeness.Unresolved, want) {
		t.Fatalf("unresolved = %+v", view.Completeness)
	}
}

func TestParseSnapshotLegacyWithoutPlatform(t *testing.T) {
	snapshot, err := ParseSnapshot([]byte(`{"ingredients":[],"products":[],"intermediates":[],"electricity":{},"pollution":{},"completeness":{},"calculator":{}}`), 1)
	if err != nil {
		t.Fatal(err)
	}
	view, err := snapshot.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.SpacePlatform == nil || view.SpacePlatform.IsSpacePlatform || snapshot.Facts().Platform != nil {
		t.Fatalf("legacy platform = %+v", view.SpacePlatform)
	}
	if view.Footprint != nil || view.BuildCost != nil {
		t.Fatalf("legacy footprint = %+v, build cost = %+v", view.Footprint, view.BuildCost)
	}
}

func TestParseSnapshotRejectsUnknownVersionAndBadShape(t *testing.T) {
	if _, err := ParseSnapshot([]byte(`{}`), 2); err == nil {
		t.Fatal("unknown schema version accepted")
	}
	if _, err := ParseSnapshot([]byte(`{`), 1); err == nil {
		t.Fatal("malformed snapshot accepted")
	}
	if _, err := ParseSnapshot([]byte(`{}`), 1); err == nil {
		t.Fatal("empty snapshot accepted")
	}
	bad := `{"ingredients":[],"products":[],"intermediates":[],"electricity":{},"pollution":{},"space_platform":[]}`
	if _, err := ParseSnapshot([]byte(bad), 1); err == nil {
		t.Fatal("non-object platform accepted")
	}
}
