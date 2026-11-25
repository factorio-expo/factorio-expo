// Package challenge holds a design challenge's rules: the goal (the one ranked
// parameter), the environment and the restrictions, their validation, the
// check of one blueprint against them and their plain-language text
// (docs/api.md#challenges). It reads stored facts only and knows
// nothing about the database.
package challenge

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"factorio-expo/internal/icontoken"
)

// Content is which game content an entry may use.
const (
	ContentVanilla  = "vanilla"
	ContentSpaceAge = "space-age"
)

// Kind is which kind of blueprint an entry must be.
const (
	KindAny      = "any"
	KindFactory  = "factory"
	KindPlatform = "platform"
)

// Direction says which goal values win.
const (
	DirectionMax = "max"
	DirectionMin = "min"
)

// Restriction operators.
const (
	OpLess      = "<"
	OpAtMost    = "<="
	OpEqual     = "="
	OpAtLeast   = ">="
	OpGreater   = ">"
	equalMargin = 1e-9
)

// Ops lists the restriction operators.
var Ops = []string{OpLess, OpAtMost, OpEqual, OpAtLeast, OpGreater}

// MaxRestrictions bounds a rule set.
const MaxRestrictions = 30

// ErrInvalidRules wraps what is wrong with a rule set; its message is safe to
// show to staff.
var ErrInvalidRules = errors.New("invalid challenge rules")

var prototypeName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,99}$`)

// Rules are a challenge's checkable facts, stored as challenges.rules.
type Rules struct {
	// Content is vanilla or space-age (Space Age with quality, elevated rails
	// and recycler). Other mods never rank.
	Content string `json:"content"`
	// Kind is factory, platform or any.
	Kind         string        `json:"kind"`
	Goal         Goal          `json:"goal"`
	Restrictions []Restriction `json:"restrictions"`
	// Imports limits what the blueprint may take from outside; nil allows
	// any import (see ImportRule).
	Imports *ImportRule `json:"imports,omitempty"`
}

// Target names what a parameter measures, when it needs one: a material (type
// item or fluid, name, quality: empty means every quality), an entity or a
// recipe (name, quality) or a raw resource (name).
type Target struct {
	Type    string `json:"type,omitempty"`
	Name    string `json:"name,omitempty"`
	Quality string `json:"quality,omitempty"`
}

// Goal is the ranked parameter and whether higher or lower wins.
type Goal struct {
	Param     string `json:"param"`
	Target    Target `json:"target"`
	Direction string `json:"direction"`
}

// Restriction is a pass/fail limit on a parameter: "beacon count = 0",
// "footprint area <= 400".
type Restriction struct {
	Param  string  `json:"param"`
	Target Target  `json:"target"`
	Op     string  `json:"op"`
	Value  float64 `json:"value"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRules, fmt.Sprintf(format, args...))
}

// Normalize fills the defaults and validates the rules. The result is what
// gets stored.
func (r Rules) Normalize() (Rules, error) {
	switch r.Content {
	case ContentVanilla, ContentSpaceAge:
	case "":
		r.Content = ContentSpaceAge
	default:
		return r, invalid("content is vanilla or space-age")
	}
	switch r.Kind {
	case KindAny, KindFactory, KindPlatform:
	case "":
		r.Kind = KindAny
	default:
		return r, invalid("kind is any, factory or platform")
	}

	goal, err := normalizeTarget(r.Goal.Param, r.Goal.Target, "the goal")
	if err != nil {
		return r, err
	}
	r.Goal.Target = goal
	if r.Goal.Direction != DirectionMax && r.Goal.Direction != DirectionMin {
		return r, invalid("the goal direction is max or min")
	}
	if platformOnly(r.Goal.Param) && r.Kind == KindFactory {
		return r, invalid("a platform goal needs kind platform or any")
	}

	if len(r.Restrictions) > MaxRestrictions {
		return r, invalid("at most %d restrictions", MaxRestrictions)
	}
	restrictions := make([]Restriction, 0, len(r.Restrictions))
	for i, restriction := range r.Restrictions {
		where := fmt.Sprintf("restriction %d", i+1)
		target, err := normalizeTarget(restriction.Param, restriction.Target, where)
		if err != nil {
			return r, err
		}
		restriction.Target = target
		if !slices.Contains(Ops, restriction.Op) {
			return r, invalid("%s: the operator is one of %s", where, strings.Join(Ops, " "))
		}
		if !finite(restriction.Value) {
			return r, invalid("%s: the value must be a number", where)
		}
		restrictions = append(restrictions, restriction)
	}
	r.Restrictions = restrictions
	imports, err := normalizeImports(r.Imports)
	if err != nil {
		return r, err
	}
	r.Imports = imports
	return r, nil
}

// normalizeTarget checks a parameter and keeps only the target fields it
// uses.
func normalizeTarget(param string, target Target, where string) (Target, error) {
	definition, ok := paramByName[param]
	if !ok {
		return Target{}, invalid("%s: unknown parameter %q", where, param)
	}
	target.Name = strings.TrimSpace(target.Name)
	if target.Quality != "" && !icontoken.ValidQuality(target.Quality) {
		return Target{}, invalid("%s: unknown quality %q", where, target.Quality)
	}
	switch definition.Target {
	case TargetNone:
		return Target{}, nil
	case TargetMaterial:
		if target.Type != "item" && target.Type != "fluid" {
			return Target{}, invalid("%s: pick an item or a fluid", where)
		}
		if !prototypeName.MatchString(target.Name) {
			return Target{}, invalid("%s: pick an item or a fluid", where)
		}
		if target.Type == "fluid" {
			target.Quality = ""
		}
	case TargetEntity, TargetRecipe:
		target.Type = ""
		if target.Name == "" && definition.Target == TargetEntity {
			return Target{Quality: target.Quality}, nil
		}
		if !prototypeName.MatchString(target.Name) {
			return Target{}, invalid("%s: pick an %s", where, map[TargetKind]string{TargetEntity: "entity", TargetRecipe: "recipe"}[definition.Target])
		}
	case TargetResource:
		target.Type, target.Quality = "", ""
		if target.Name != "" && !prototypeName.MatchString(target.Name) {
			return Target{}, invalid("%s: %q is not a resource name", where, target.Name)
		}
	}
	return target, nil
}

// qualityRank is a quality's place, lowest first; -1 when unknown.
func qualityRank(quality string) int {
	if quality == "" {
		quality = "normal"
	}
	return slices.Index(icontoken.Qualities, quality)
}

// passes reports whether value meets op against limit.
func passes(op string, value, limit float64) bool {
	equal := abs(value-limit) <= equalMargin*max(1, abs(limit))
	switch op {
	case OpLess:
		return value < limit && !equal
	case OpAtMost:
		return value < limit || equal
	case OpEqual:
		return equal
	case OpAtLeast:
		return value > limit || equal
	case OpGreater:
		return value > limit && !equal
	}
	return false
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
