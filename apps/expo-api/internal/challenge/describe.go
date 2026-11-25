package challenge

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"factorio-expo/internal/icontoken"
)

// The challenge page shows the rules as lines of text with icon tokens
// ([item=x,quality=rare]), which the web renders as icons.

// Description is the rules in words.
type Description struct {
	Goal         string   `json:"goal" doc:"The goal in words, with icon tokens."`
	Restrictions []string `json:"restrictions" doc:"Every rule in words, with icon tokens."`
}

// opWords are the restriction operators in words.
var opWords = map[string]string{
	OpLess: "under", OpAtMost: "at most", OpEqual: "exactly", OpAtLeast: "at least", OpGreater: "over",
}

// Describe writes the rules in words.
func Describe(rules Rules, options Options) Description {
	direction := "Highest"
	if rules.Goal.Direction == DirectionMin {
		direction = "Lowest"
	}
	description := Description{
		Goal:         direction + " " + lowerFirst(describeParam(rules.Goal.Param, rules.Goal.Target)),
		Restrictions: []string{},
	}
	add := func(format string, args ...any) {
		description.Restrictions = append(description.Restrictions, fmt.Sprintf(format, args...))
	}
	add("A single blueprint, not a book")
	if rules.Content == ContentVanilla {
		add("Base game only: no Space Age content, no mods")
	} else {
		add("Base game and Space Age only: no other mods")
	}
	switch rules.Kind {
	case KindPlatform:
		add("A space platform")
	case KindFactory:
		add("Not a space platform")
	}
	if options.RequireNewRevisions {
		add("Uploaded after the challenge started (an older entry counts once it gets a new revision)")
	}
	for _, restriction := range rules.Restrictions {
		add("%s: %s %s", describeParam(restriction.Param, restriction.Target),
			opWords[restriction.Op], FormatValue(restriction.Param, restriction.Value))
	}
	if rules.Imports != nil {
		add("%s", rules.Imports.describe())
	}
	add("The analysis must be complete")
	return description
}

// describeParam names a parameter with its target ("Output rate of
// [item=gear]").
func describeParam(name string, target Target) string {
	param, ok := paramByName[name]
	if !ok {
		return name
	}
	switch param.Target {
	case TargetMaterial:
		return param.Label + " of " + materialToken(target.Type, target.Name, target.Quality)
	case TargetRecipe:
		return param.Label + " " + token("recipe", target.Name, target.Quality)
	case TargetEntity:
		if target.Name == "" {
			if target.Quality != "" {
				return param.Label + " (" + target.Quality + ")"
			}
			return param.Label
		}
		return param.Label + " of " + token("entity", target.Name, target.Quality)
	case TargetResource:
		if target.Name == "" {
			return param.Label + ", all resources"
		}
		return param.Label + ", " + materialToken("item", target.Name, "")
	}
	return param.Label
}

// FormatValue writes a parameter's value with its unit.
func FormatValue(name string, value float64) string {
	text := formatNumber(value)
	if param, ok := paramByName[name]; ok && param.Unit != "" {
		if strings.HasPrefix(param.Unit, "/") {
			return text + param.Unit
		}
		return text + " " + param.Unit
	}
	return text
}

// formatNumber keeps four significant digits, without an exponent.
func formatNumber(value float64) string {
	if value == 0 || !finite(value) {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
	digits := max(3-int(math.Floor(math.Log10(math.Abs(value)))), 0)
	text := strconv.FormatFloat(value, 'f', digits, 64)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text
}

// materialToken is the icon token of an item (with its quality, when one is
// named) or a fluid.
func materialToken(kind, name, quality string) string {
	if kind == "fluid" {
		return token("fluid", name, "")
	}
	return token("item", name, quality)
}

func token(kind, name, quality string) string {
	return icontoken.Format(icontoken.Token{Type: kind, Name: name, Quality: quality}, false)
}

func lowerFirst(text string) string {
	if text == "" {
		return text
	}
	return strings.ToLower(text[:1]) + text[1:]
}
