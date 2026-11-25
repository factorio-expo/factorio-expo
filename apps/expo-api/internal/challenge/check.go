package challenge

import (
	"fmt"
	"time"

	"factorio-expo/apps/expo-api/internal/mods"
	"factorio-expo/internal/analysisresult"
)

// Verdict statuses.
const (
	// StatusPending: the analysis has not finished.
	StatusPending = "pending"
	// StatusAccepted: every rule passes and the goal has a value.
	StatusAccepted = "accepted"
	// StatusRejected: a rule failed; Reasons say which.
	StatusRejected = "rejected"
)

// Analysis states of a blueprint.
const (
	AnalysisPending = "pending"
	AnalysisFailed  = "failed"
	AnalysisDone    = "done"
)

// Blueprint is what the check reads about one submitted blueprint: stored
// facts of its scored revision.
type Blueprint struct {
	// Single is false when the revision is a book or a planner.
	Single            bool
	RevisionCreatedAt time.Time
	Entities          []EntityCount
	// Recipes count the entities set to each recipe and quality.
	Recipes []EntityCount
	// Mods is the stored mod detection; nil when there is none yet.
	Mods *mods.Detection
	// Analysis is pending, failed or done; Partial and Snapshot are read only
	// when done.
	Analysis string
	Partial  bool
	Snapshot *analysisresult.Snapshot
}

// Options are the challenge's settings the check needs besides its rules.
type Options struct {
	StartsAt time.Time
	// RequireNewRevisions: the scored revision must be made after StartsAt.
	RequireNewRevisions bool
}

// Verdict is the outcome of the check.
type Verdict struct {
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
	// Score is the goal's value; set only when accepted.
	Score *float64 `json:"score"`
}

// structural reports whether a parameter is read from the blueprint itself,
// so it is checked before the analysis finishes.
func structural(param string) bool {
	target := paramByName[param].Target
	return target == TargetEntity || target == TargetRecipe
}

// Check checks one blueprint against the rules. Rules read from the
// blueprint itself come first, so one that breaks them is rejected before
// its analysis finishes.
func Check(rules Rules, options Options, b Blueprint) Verdict {
	var reasons []string
	fail := func(format string, args ...any) { reasons = append(reasons, fmt.Sprintf(format, args...)) }

	if !b.Single {
		fail("the entry must be a single blueprint, not a book")
	}
	if options.RequireNewRevisions && b.RevisionCreatedAt.Before(options.StartsAt) {
		fail("the blueprint was uploaded before the challenge started; upload a new revision")
	}
	if b.Mods != nil {
		if reason := contentProblem(rules.Content, *b.Mods); reason != "" {
			fail("%s", reason)
		}
	}
	structuralValues := values{entities: b.Entities, recipes: b.Recipes}
	for _, restriction := range rules.Restrictions {
		if structural(restriction.Param) {
			checkRestriction(restriction, structuralValues, fail)
		}
	}

	if len(reasons) > 0 {
		return Verdict{Status: StatusRejected, Reasons: reasons}
	}
	if b.Mods == nil {
		return Verdict{Status: StatusPending, Reasons: []string{}}
	}
	switch b.Analysis {
	case AnalysisPending:
		return Verdict{Status: StatusPending, Reasons: []string{}}
	case AnalysisFailed:
		return Verdict{Status: StatusRejected, Reasons: []string{"the analysis failed"}}
	}
	if b.Partial || b.Snapshot == nil {
		return Verdict{Status: StatusRejected, Reasons: []string{"the analysis is incomplete: some entities could not be calculated"}}
	}

	analysed := analysedValues(b)
	isPlatform := analysed.facts.Platform != nil
	switch {
	case rules.Kind == KindPlatform && !isPlatform:
		fail("the entry must be a space platform")
	case rules.Kind == KindFactory && isPlatform:
		fail("the entry must not be a space platform")
	}
	for _, restriction := range rules.Restrictions {
		if !structural(restriction.Param) {
			checkRestriction(restriction, analysed, fail)
		}
	}
	if rules.Imports != nil {
		checkImports(*rules.Imports, analysed.facts.Imports, fail)
	}
	score, ok := read(rules.Goal.Param, rules.Goal.Target, analysed)
	if !ok {
		fail("%s cannot be measured for this blueprint", describeParam(rules.Goal.Param, rules.Goal.Target))
	}
	if len(reasons) > 0 {
		return Verdict{Status: StatusRejected, Reasons: reasons}
	}
	return Verdict{Status: StatusAccepted, Reasons: []string{}, Score: &score}
}

func analysedValues(b Blueprint) values {
	facts := b.Snapshot.Facts()
	return values{facts: &facts, hasBuildCost: b.Snapshot.BuildCost != nil, entities: b.Entities, recipes: b.Recipes}
}

func read(param string, target Target, v values) (float64, bool) {
	definition, ok := paramByName[param]
	if !ok {
		return 0, false
	}
	v.target = target
	return definition.value(v)
}

func checkRestriction(restriction Restriction, v values, fail func(string, ...any)) {
	value, ok := read(restriction.Param, restriction.Target, v)
	subject := describeParam(restriction.Param, restriction.Target)
	if !ok {
		fail("%s cannot be measured for this blueprint", subject)
		return
	}
	if !passes(restriction.Op, value, restriction.Value) {
		fail("%s is %s; it must be %s %s", subject, FormatValue(restriction.Param, value),
			opWords[restriction.Op], FormatValue(restriction.Param, restriction.Value))
	}
}

// contentProblem says why a mod detection breaks the content rule; "" when it
// does not.
func contentProblem(content string, detection mods.Detection) string {
	if detection.IsVanilla() {
		return ""
	}
	thirdParty := len(detection.UnknownEntities) > 0 || len(detection.UnknownRecipes) > 0
	for _, match := range detection.Modsets {
		if !match.Builtin {
			thirdParty = true
		}
	}
	if thirdParty {
		return "uses mods; only the base game and Space Age rank"
	}
	if content == ContentVanilla {
		return "uses Space Age content; this challenge is vanilla only"
	}
	return ""
}

// Better reports whether score a ranks above score b under the direction.
func Better(direction string, a, b float64) bool {
	if direction == DirectionMin {
		return a < b
	}
	return a > b
}

// GoalValue reads the goal of a blueprint whatever its restrictions say (a
// staff override accepts it): only a done, complete analysis has one.
func GoalValue(rules Rules, b Blueprint) (float64, bool) {
	if b.Analysis != AnalysisDone || b.Partial || b.Snapshot == nil {
		return 0, false
	}
	return read(rules.Goal.Param, rules.Goal.Target, analysedValues(b))
}
