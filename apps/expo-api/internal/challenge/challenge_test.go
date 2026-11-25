package challenge

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/mods"
	"factorio-expo/internal/analysisresult"
)

func goldenSnapshot(t *testing.T) *analysisresult.Snapshot {
	t.Helper()
	data, err := os.ReadFile("../../../../test-data/analysis/analysis-succeeded.json")
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := analysisresult.Parse(data, "pj_golden")
	if err != nil || result == nil {
		t.Fatalf("parse: %v", err)
	}
	stored, err := result.Stored()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := analysisresult.ParseSnapshot(stored.Snapshot, stored.SchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func circuitRules() Rules {
	return Rules{Goal: Goal{
		Param: "rate_out", Direction: DirectionMax,
		Target: Target{Type: "item", Name: "electronic-circuit", Quality: "rare"},
	}}
}

func analysedBlueprint(t *testing.T) Blueprint {
	return Blueprint{
		Single: true, RevisionCreatedAt: time.Unix(2000, 0),
		Entities: []EntityCount{{Name: "assembling-machine-2", Quality: "normal", Count: 2}, {Name: "beacon", Quality: "rare", Count: 1}},
		Recipes:  []EntityCount{{Name: "electronic-circuit", Quality: "normal", Count: 2}},
		Mods:     &mods.Detection{}, Analysis: AnalysisDone, Snapshot: goldenSnapshot(t),
	}
}

func TestNormalizeDefaultsAndRefusals(t *testing.T) {
	rules, err := circuitRules().Normalize()
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if rules.Content != ContentSpaceAge || rules.Kind != KindAny || rules.Restrictions == nil {
		t.Fatalf("defaults = %+v", rules)
	}
	numeric := Rules{Goal: Goal{Param: "footprint_area", Direction: DirectionMin, Target: Target{Type: "item", Name: "x"}}}
	if numeric, err = numeric.Normalize(); err != nil || numeric.Goal.Target != (Target{}) {
		t.Fatalf("numeric goal keeps no target: %+v, %v", numeric.Goal, err)
	}

	broken := []func(*Rules){
		func(r *Rules) { r.Goal.Param = "nope" },
		func(r *Rules) { r.Goal.Direction = "" },
		func(r *Rules) { r.Goal.Target.Type = "" },
		func(r *Rules) { r.Goal.Target.Quality = "shiny" },
		func(r *Rules) { r.Kind = KindFactory; r.Goal = Goal{Param: "platform_speed", Direction: DirectionMax} },
		func(r *Rules) { r.Restrictions = []Restriction{{Param: "footprint_area", Op: "!=", Value: 1}} },
		func(r *Rules) { r.Restrictions = []Restriction{{Param: "recipe_count", Op: "=", Value: 1}} },
		func(r *Rules) {
			r.Restrictions = []Restriction{{Param: "entity_count", Target: Target{Name: "Bad Name"}, Op: "=", Value: 0}}
		},
	}
	for i, breakIt := range broken {
		rules := circuitRules()
		breakIt(&rules)
		if _, err := rules.Normalize(); !errors.Is(err, ErrInvalidRules) {
			t.Errorf("case %d: %v, want invalid", i, err)
		}
	}
}

func TestPasses(t *testing.T) {
	cases := []struct {
		op          string
		value, want float64
		ok          bool
	}{
		{OpLess, 1, 2, true}, {OpLess, 2, 2, false},
		{OpAtMost, 2, 2, true}, {OpAtMost, 3, 2, false},
		{OpEqual, 0.1 + 0.2, 0.3, true}, {OpEqual, 1, 2, false},
		{OpAtLeast, 2, 2, true}, {OpAtLeast, 1, 2, false},
		{OpGreater, 3, 2, true}, {OpGreater, 2, 2, false},
	}
	for _, c := range cases {
		if got := passes(c.op, c.value, c.want); got != c.ok {
			t.Errorf("%v %s %v = %v, want %v", c.value, c.op, c.want, got, c.ok)
		}
	}
}

func TestCheckScoresAndRejects(t *testing.T) {
	rules, _ := circuitRules().Normalize()
	verdict := Check(rules, Options{}, analysedBlueprint(t))
	if verdict.Status != StatusAccepted || verdict.Score == nil || *verdict.Score != 0.5 {
		t.Fatalf("verdict = %+v, want accepted at 0.5", verdict)
	}

	pending := analysedBlueprint(t)
	pending.Analysis, pending.Snapshot = AnalysisPending, nil
	if got := Check(rules, Options{}, pending); got.Status != StatusPending {
		t.Fatalf("pending analysis = %+v", got)
	}
	// A structural rule rejects before the analysis is done.
	blocked := rules
	blocked.Restrictions = []Restriction{{Param: "entity_count", Target: Target{Name: "beacon"}, Op: OpEqual, Value: 0}}
	if got := Check(blocked, Options{}, pending); got.Status != StatusRejected {
		t.Fatalf("blocked entity while pending = %+v", got)
	}
	partial := analysedBlueprint(t)
	partial.Partial = true
	if got := Check(rules, Options{}, partial); got.Status != StatusRejected {
		t.Fatalf("partial analysis = %+v", got)
	}

	restrict := func(r ...Restriction) func(*Rules) { return func(rules *Rules) { rules.Restrictions = r } }
	cases := map[string]struct {
		rules   func(*Rules)
		options Options
		change  func(*Blueprint)
		reason  string
	}{
		"book":         {change: func(b *Blueprint) { b.Single = false }, reason: "single blueprint"},
		"old revision": {options: Options{StartsAt: time.Unix(3000, 0), RequireNewRevisions: true}, reason: "before the challenge started"},
		"modded": {change: func(b *Blueprint) {
			b.Mods = &mods.Detection{Modsets: []mods.Match{{Name: "krastorio2"}}}
		}, reason: "uses mods"},
		"vanilla": {rules: func(r *Rules) { r.Content = ContentVanilla }, change: func(b *Blueprint) {
			b.Mods = &mods.Detection{Modsets: []mods.Match{{Name: "quality", Builtin: true}}}
		}, reason: "vanilla only"},
		"entity cap": {rules: restrict(Restriction{Param: "entity_count", Target: Target{Name: "assembling-machine-2"}, Op: OpAtMost, Value: 1}),
			reason: "Entity count of [entity=assembling-machine-2] is 2; it must be at most 1"},
		"entity quality": {rules: restrict(Restriction{Param: "entity_count", Target: Target{Quality: "rare"}, Op: OpEqual, Value: 0}),
			reason: "Entity count (rare) is 1"},
		"recipe": {rules: restrict(Restriction{Param: "recipe_count", Target: Target{Name: "electronic-circuit"}, Op: OpLess, Value: 1}),
			reason: "Entities set to recipe [recipe=electronic-circuit] is 2"},
		"platform": {rules: func(r *Rules) { r.Kind = KindPlatform }, reason: "must be a space platform"},
		"footprint": {rules: restrict(Restriction{Param: "footprint_area", Op: OpAtMost, Value: 10}),
			reason: "Footprint area is 84 tiles; it must be at most 10 tiles"},
		"input": {rules: restrict(Restriction{Param: "rate_in", Target: Target{Type: "fluid", Name: "water"}, Op: OpEqual, Value: 0}),
			reason: "Input rate of [fluid=water] is 12.5/s"},
	}
	for name, c := range cases {
		rules := circuitRules()
		if c.rules != nil {
			c.rules(&rules)
		}
		rules, err := rules.Normalize()
		if err != nil {
			t.Fatalf("%s: normalize: %v", name, err)
		}
		b := analysedBlueprint(t)
		if c.change != nil {
			c.change(&b)
		}
		verdict := Check(rules, c.options, b)
		if verdict.Status != StatusRejected || !strings.Contains(strings.Join(verdict.Reasons, "|"), c.reason) {
			t.Errorf("%s: verdict = %+v, want a reason with %q", name, verdict, c.reason)
		}
	}
}

func TestDescribeAndFormat(t *testing.T) {
	rules := circuitRules()
	rules.Restrictions = []Restriction{
		{Param: "power_consumption", Op: OpAtMost, Value: 100},
		{Param: "entity_count", Target: Target{Name: "beacon"}, Op: OpEqual, Value: 0},
	}
	rules, err := rules.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	description := Describe(rules, Options{RequireNewRevisions: true})
	if description.Goal != "Highest output rate of [item=electronic-circuit,quality=rare]" {
		t.Fatalf("goal = %q", description.Goal)
	}
	text := strings.Join(description.Restrictions, "\n")
	for _, want := range []string{"Power used: at most 100 MW", "Entity count of [entity=beacon]: exactly 0", "Uploaded after the challenge started", "A single blueprint, not a book"} {
		if !strings.Contains(text, want) {
			t.Errorf("restrictions %q lack %q", text, want)
		}
	}
	for value, want := range map[float64]string{0.5: "0.5", 1234.56: "1235", 0.012345: "0.01235", 12.5: "12.5", 0: "0"} {
		if got := formatNumber(value); got != want {
			t.Errorf("formatNumber(%v) = %q, want %q", value, got, want)
		}
	}
}

func TestMeasureReadsStructuralRulesBeforeTheAnalysis(t *testing.T) {
	rules := Rules{
		Content: ContentVanilla,
		Kind:    KindFactory,
		Goal:    Goal{Param: "power_consumption", Direction: DirectionMin},
		Restrictions: []Restriction{
			{Param: "entity_count", Op: OpAtMost, Value: 10},
			{Param: "power_consumption", Op: OpAtMost, Value: 5},
		},
	}
	lines := Measure(rules, Blueprint{
		Single:   true,
		Entities: []EntityCount{{Name: "assembling-machine-1", Count: 4}},
		Analysis: AnalysisPending,
	})
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 2 restrictions and the goal", len(lines))
	}
	if lines[0].Value != "4" || !lines[0].Pass {
		t.Errorf("entity count line = %+v, want 4 passing", lines[0])
	}
	if lines[1].Value != "" || lines[1].Pass {
		t.Errorf("power line = %+v, want unmeasured before the analysis", lines[1])
	}
	if !lines[2].Goal || lines[2].Value != "" {
		t.Errorf("goal line = %+v, want the goal, unmeasured", lines[2])
	}
}
