package challenge

import (
	"strings"
	"testing"

	"factorio-expo/internal/analysisresult"
)

func TestNormalizeImports(t *testing.T) {
	rules, err := circuitRules().Normalize()
	if err != nil || rules.Imports != nil {
		t.Fatalf("no rule must stay nil: %+v, %v", rules.Imports, err)
	}
	rules = circuitRules()
	rules.Imports = &ImportRule{}
	if rules, err = rules.Normalize(); err != nil || rules.Imports == nil || rules.Imports.Allowed == nil || len(rules.Imports.Allowed) != 0 {
		t.Fatalf("an empty list is a rule that allows nothing: %+v, %v", rules.Imports, err)
	}
	rules.Imports = &ImportRule{Allowed: []Target{
		{Type: "fluid", Name: "water", Quality: "rare"}, {Type: "fluid", Name: "water"},
		{Type: "item", Name: "iron-plate"}, {Type: "item", Name: "iron-plate", Quality: "rare"},
	}}
	if rules, err = rules.Normalize(); err != nil {
		t.Fatal(err)
	}
	want := []Target{{Type: "fluid", Name: "water"}, {Type: "item", Name: "iron-plate"}, {Type: "item", Name: "iron-plate", Quality: "rare"}}
	if len(rules.Imports.Allowed) != len(want) {
		t.Fatalf("allowed = %+v, want %+v (fluid quality dropped, repeats merged)", rules.Imports.Allowed, want)
	}
	for i := range want {
		if rules.Imports.Allowed[i] != want[i] {
			t.Fatalf("allowed = %+v, want %+v", rules.Imports.Allowed, want)
		}
	}
	for name, target := range map[string]Target{
		"no type": {Name: "iron-plate"}, "bad name": {Type: "item", Name: "Iron Plate"}, "bad quality": {Type: "item", Name: "iron-plate", Quality: "shiny"},
	} {
		bad := circuitRules()
		bad.Imports = &ImportRule{Allowed: []Target{target}}
		if _, err := bad.Normalize(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestImportRuleRejectsWhatIsNotMadeInside(t *testing.T) {
	check := func(imports *ImportRule) Verdict {
		rules := circuitRules()
		rules.Imports = imports
		rules, err := rules.Normalize()
		if err != nil {
			t.Fatal(err)
		}
		return Check(rules, Options{}, analysedBlueprint(t))
	}
	// The golden blueprint imports iron gear wheels (0.17/s) and water (12.5/s).
	if got := check(nil); got.Status != StatusAccepted {
		t.Fatalf("no rule = %+v", got)
	}
	got := check(&ImportRule{})
	reasons := strings.Join(got.Reasons, "|")
	if got.Status != StatusRejected || !strings.Contains(reasons, "imports [fluid=water] at 12.5/s") || !strings.Contains(reasons, "[item=iron-gear-wheel] at 0.1667/s") {
		t.Fatalf("no imports allowed = %+v", got)
	}
	if got := check(&ImportRule{Allowed: []Target{{Type: "fluid", Name: "water"}}}); got.Status != StatusRejected || len(got.Reasons) != 1 {
		t.Fatalf("water only must still reject the gear wheels once: %+v", got)
	}
	both := &ImportRule{Allowed: []Target{{Type: "fluid", Name: "water"}, {Type: "item", Name: "iron-gear-wheel"}}}
	if got := check(both); got.Status != StatusAccepted {
		t.Fatalf("allowed = %+v", got)
	}
	// A rare-only allowance does not cover normal gear wheels.
	rare := &ImportRule{Allowed: []Target{{Type: "fluid", Name: "water"}, {Type: "item", Name: "iron-gear-wheel", Quality: "rare"}}}
	if got := check(rare); got.Status != StatusRejected {
		t.Fatalf("a quality-specific allowance covers other qualities: %+v", got)
	}
}

func TestImportToleranceAndShortfalls(t *testing.T) {
	rule := ImportRule{Allowed: []Target{{Type: "item", Name: "iron-plate"}}}
	imports := []analysisresult.Import{
		{Type: "item", Name: "iron-plate", Quality: "normal", PerSecond: 50},
		{Type: "item", Name: "copper-cable", Quality: "normal", PerSecond: 1e-9},
		{Type: "item", Name: "electronic-circuit", Quality: "normal", PerSecond: 0.4},
		{Type: "item", Name: "electronic-circuit", Quality: "normal", PerSecond: 0.6},
	}
	bad := rule.disallowed(imports)
	if len(bad) != 1 || bad[0].Name != "electronic-circuit" || bad[0].PerSecond != 1 {
		t.Fatalf("disallowed = %+v, want only the circuits, summed, with the float noise ignored", bad)
	}
	if value, pass := measureImports(rule, imports); pass || value != "electronic-circuit 1/s" {
		t.Fatalf("measure = %q, %v", value, pass)
	}
	if value, pass := measureImports(rule, imports[:2]); !pass || value != "none outside the list" {
		t.Fatalf("measure = %q, %v", value, pass)
	}
}

func TestFactsImportsKeepOnlyWhatComesFromOutside(t *testing.T) {
	snapshot := &analysisresult.Snapshot{
		Ingredients: []analysisresult.Material{{Type: "item", Name: "iron-plate", Quality: "normal", InputPerSecond: 3}},
		Intermediates: []analysisresult.Material{
			{Type: "item", Name: "copper-cable", Quality: "normal", InputPerSecond: 6, OutputPerSecond: 4, DifferencePerSecond: -2},
			{Type: "item", Name: "gear", Quality: "normal", InputPerSecond: 1, OutputPerSecond: 5, DifferencePerSecond: 4},
		},
	}
	imports := snapshot.Facts().Imports
	if len(imports) != 2 || imports[0].Name != "copper-cable" || imports[0].PerSecond != 2 || imports[1].Name != "iron-plate" || imports[1].PerSecond != 3 {
		t.Fatalf("imports = %+v, want the cable shortfall and the plates, not the gear surplus", imports)
	}
}

func TestDescribeAndMeasureImports(t *testing.T) {
	rules := circuitRules()
	rules.Imports = &ImportRule{Allowed: []Target{{Type: "item", Name: "iron-plate"}, {Type: "fluid", Name: "crude-oil"}}}
	rules, err := rules.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(Describe(rules, Options{}).Restrictions, "\n")
	if !strings.Contains(text, "Imports only [item=iron-plate] [fluid=crude-oil]") {
		t.Fatalf("description = %q", text)
	}
	rules.Imports.Allowed = nil
	if text := strings.Join(Describe(rules, Options{}).Restrictions, "\n"); !strings.Contains(text, "No imports") {
		t.Fatalf("description = %q", text)
	}
	lines := Measure(rules, analysedBlueprint(t))
	var found bool
	for _, line := range lines {
		if strings.HasPrefix(line.Rule, "No imports") {
			found = true
			if line.Pass || !strings.Contains(line.Value, "water 12.5/s") {
				t.Fatalf("import line = %+v", line)
			}
		}
	}
	if !found {
		t.Fatalf("no import line in %+v", lines)
	}
}
