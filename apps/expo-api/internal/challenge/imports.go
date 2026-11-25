package challenge

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"factorio-expo/internal/analysisresult"
	"factorio-expo/internal/icontoken"
)

// MaxAllowedImports bounds the list of an import rule.
const MaxAllowedImports = 100

// ImportTolerance is the import rate (per second) below which a material
// counts as not imported, so the float noise of a balanced factory does not
// reject it.
const ImportTolerance = 1e-6

// ImportRule limits what a blueprint may take from outside. The analysis
// reports every shortfall as an import, so a material missing from Allowed
// has to be made inside the blueprint at the rate it is used. An empty list
// allows no import at all. A challenge without the rule allows any import.
type ImportRule struct {
	// Allowed are the materials that may be imported: an item (any quality
	// when none is named) or a fluid.
	Allowed []Target `json:"allowed"`
}

// normalizeImports validates the rule's list and drops repeats.
func normalizeImports(rule *ImportRule) (*ImportRule, error) {
	if rule == nil {
		return nil, nil
	}
	if len(rule.Allowed) > MaxAllowedImports {
		return nil, invalid("at most %d allowed imports", MaxAllowedImports)
	}
	allowed := make([]Target, 0, len(rule.Allowed))
	for i, target := range rule.Allowed {
		where := fmt.Sprintf("allowed import %d", i+1)
		target.Name = strings.TrimSpace(target.Name)
		if (target.Type != "item" && target.Type != "fluid") || !prototypeName.MatchString(target.Name) {
			return nil, invalid("%s: pick an item or a fluid", where)
		}
		if target.Quality != "" && !icontoken.ValidQuality(target.Quality) {
			return nil, invalid("%s: unknown quality %q", where, target.Quality)
		}
		if target.Type == "fluid" {
			target.Quality = ""
		}
		if !slices.Contains(allowed, target) {
			allowed = append(allowed, target)
		}
	}
	return &ImportRule{Allowed: allowed}, nil
}

// allows reports whether the rule lets the material in.
func (rule ImportRule) allows(material analysisresult.Import) bool {
	return slices.ContainsFunc(rule.Allowed, func(target Target) bool {
		return target.Type == material.Type && target.Name == material.Name &&
			(target.Quality == "" || target.Quality == material.Quality)
	})
}

// disallowed returns the imports the rule does not allow, a material once
// (its quality rows kept apart), the largest first.
func (rule ImportRule) disallowed(imports []analysisresult.Import) []analysisresult.Import {
	type key struct{ kind, name, quality string }
	sums := map[key]float64{}
	for _, material := range imports {
		if !rule.allows(material) {
			sums[key{material.Type, material.Name, material.Quality}] += material.PerSecond
		}
	}
	var out []analysisresult.Import
	for k, rate := range sums {
		if rate > ImportTolerance {
			out = append(out, analysisresult.Import{Type: k.kind, Name: k.name, Quality: k.quality, PerSecond: rate})
		}
	}
	slices.SortFunc(out, func(a, b analysisresult.Import) int {
		return cmp.Or(cmp.Compare(b.PerSecond, a.PerSecond), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Quality, b.Quality))
	})
	return out
}

// describe writes the rule in words, with icon tokens.
func (rule ImportRule) describe() string {
	if len(rule.Allowed) == 0 {
		return "No imports: everything is made inside the blueprint"
	}
	tokens := make([]string, len(rule.Allowed))
	for i, target := range rule.Allowed {
		tokens[i] = materialToken(target.Type, target.Name, target.Quality)
	}
	return "Imports only " + strings.Join(tokens, " ") + ": everything else is made inside the blueprint"
}

// checkImports fails once per import the rule does not allow.
func checkImports(rule ImportRule, imports []analysisresult.Import, fail func(string, ...any)) {
	for _, material := range rule.disallowed(imports) {
		fail("imports %s at %s/s, which is not allowed; make it inside the blueprint",
			materialToken(material.Type, material.Name, material.Quality), formatNumber(material.PerSecond))
	}
}

// measureImports is the rule measured: what the blueprint imports that the
// rule does not allow.
func measureImports(rule ImportRule, imports []analysisresult.Import) (value string, pass bool) {
	bad := rule.disallowed(imports)
	if len(bad) == 0 {
		return "none outside the list", true
	}
	parts := make([]string, len(bad))
	for i, material := range bad {
		parts[i] = fmt.Sprintf("%s %s/s", material.Name, formatNumber(material.PerSecond))
	}
	return strings.Join(parts, ", "), false
}
