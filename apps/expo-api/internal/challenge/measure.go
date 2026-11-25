package challenge

import "fmt"

// Measurement is one rule measured on a blueprint, for the console's test
// box: what the rule says, the value found, and whether it passes.
type Measurement struct {
	// Rule is the rule in words, with icon tokens ("Footprint area: at most 100 tiles").
	Rule string `json:"rule" doc:"The rule in words, with icon tokens."`
	// Value is the measured value with its unit; empty when it cannot be
	// measured yet (the analysis is pending) or at all.
	Value string `json:"value" doc:"The value measured on the blueprint; empty when it cannot be measured."`
	// Pass is whether the value satisfies the rule; always false for the
	// goal and for a value that is not measured.
	Pass bool `json:"pass" doc:"Whether the measured value satisfies the restriction."`
	// Goal marks the goal line: its value is the score.
	Goal bool `json:"goal" doc:"Whether this line is the goal (its value is the score)."`
}

// Measure reads every restriction and the goal off one blueprint, whatever the
// verdict: restrictions on the blueprint's own entities and recipes always,
// the rest once its analysis is done and complete.
func Measure(rules Rules, b Blueprint) []Measurement {
	structuralValues := values{entities: b.Entities, recipes: b.Recipes}
	analysed, hasAnalysis := values{}, b.Analysis == AnalysisDone && !b.Partial && b.Snapshot != nil
	if hasAnalysis {
		analysed = analysedValues(b)
	}
	lines := make([]Measurement, 0, len(rules.Restrictions)+2)
	for _, restriction := range rules.Restrictions {
		line := Measurement{Rule: fmt.Sprintf("%s: %s %s", describeParam(restriction.Param, restriction.Target),
			opWords[restriction.Op], FormatValue(restriction.Param, restriction.Value))}
		source := structuralValues
		if !structural(restriction.Param) {
			source = analysed
		}
		if structural(restriction.Param) || hasAnalysis {
			if value, ok := read(restriction.Param, restriction.Target, source); ok {
				line.Value = FormatValue(restriction.Param, value)
				line.Pass = passes(restriction.Op, value, restriction.Value)
			}
		}
		lines = append(lines, line)
	}
	if rules.Imports != nil {
		line := Measurement{Rule: rules.Imports.describe()}
		if hasAnalysis {
			line.Value, line.Pass = measureImports(*rules.Imports, analysed.facts.Imports)
		}
		lines = append(lines, line)
	}
	goal := Measurement{Rule: "Goal: " + lowerFirst(Describe(rules, Options{}).Goal), Goal: true}
	if hasAnalysis {
		if value, ok := read(rules.Goal.Param, rules.Goal.Target, analysed); ok {
			goal.Value = FormatValue(rules.Goal.Param, value)
		}
	}
	return append(lines, goal)
}
