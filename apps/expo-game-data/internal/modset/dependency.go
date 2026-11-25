package modset

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/go-version"
)

// Kind is how a mod depends on another (Factorio's info.json prefixes).
type Kind string

const (
	Required Kind = ""
	// NoLoadOrder ("~") is required but does not affect load order.
	NoLoadOrder Kind = "~"
	// Plus ("+") appears on built-in mods (space-age's "+ quality"); it is
	// treated as required.
	Plus           Kind = "+"
	Optional       Kind = "?"
	HiddenOptional Kind = "(?)"
	Incompatible   Kind = "!"
)

// Dependency is one parsed info.json dependency string.
type Dependency struct {
	Kind Kind
	Name string
	// Operator and Version are empty when any version will do.
	Operator string
	Version  string
}

// IsRequired reports whether the dependency must be loaded.
func (d Dependency) IsRequired() bool {
	return d.Kind == Required || d.Kind == NoLoadOrder || d.Kind == Plus
}

var dependencyPattern = regexp.MustCompile(`^(\(\?\)|[!?~+])?\s*(.+?)(?:\s*(<=|>=|<|>|=)\s*(\S+))?$`)

// ParseDependency parses "prefix name op version", e.g. "? space-age >= 2.1.0".
// Mod names may contain spaces.
func ParseDependency(text string) (Dependency, error) {
	match := dependencyPattern.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil || match[2] == "" {
		return Dependency{}, fmt.Errorf("invalid dependency %q", text)
	}
	dependency := Dependency{Kind: Kind(match[1]), Name: match[2], Operator: match[3], Version: match[4]}
	if dependency.Version != "" {
		if _, err := version.NewVersion(dependency.Version); err != nil {
			return Dependency{}, fmt.Errorf("invalid dependency %q: %w", text, err)
		}
	}
	return dependency, nil
}

// Allows reports whether the given version satisfies the dependency's
// version condition.
func (d Dependency) Allows(candidate string) (bool, error) {
	if d.Operator == "" {
		return true, nil
	}
	have, err := version.NewVersion(candidate)
	if err != nil {
		return false, fmt.Errorf("invalid version %q of %s: %w", candidate, d.Name, err)
	}
	want, err := version.NewVersion(d.Version)
	if err != nil {
		return false, err
	}
	compared := have.Compare(want)
	switch d.Operator {
	case "<":
		return compared < 0, nil
	case "<=":
		return compared <= 0, nil
	case "=":
		return compared == 0, nil
	case ">=":
		return compared >= 0, nil
	case ">":
		return compared > 0, nil
	}
	return false, fmt.Errorf("unknown operator %q", d.Operator)
}

func (d Dependency) String() string {
	text := d.Name
	if d.Kind != Required {
		text = string(d.Kind) + " " + text
	}
	if d.Operator != "" {
		text += " " + d.Operator + " " + d.Version
	}
	return text
}
