// Package settings defines the global settings staff change at runtime from the
// console (docs/admin.md#settings): the emergency switches and the site
// banner. The key/value rows live in the global_settings table; this package
// owns which keys exist, their types, defaults and validation, and a short
// cache for the hot path (every upload, sign-up and comment checks a switch).
package settings

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The setting keys. A key is added here and in Definitions; a missing row
// means its default, so adding one needs no data migration.
const (
	// AllowUploads pauses every new entry, revision, quick preview and publish.
	AllowUploads = "ALLOW_UPLOADS"
	// AllowAnonymousUploads pauses the same for signed-out visitors only.
	AllowAnonymousUploads = "ALLOW_ANONYMOUS_UPLOADS"
	// AllowSignups pauses account creation (provider and passkey sign-ups).
	AllowSignups = "ALLOW_SIGNUPS"
	// AllowComments pauses posting new comments.
	AllowComments = "ALLOW_COMMENTS"
	// SiteBanner is the text shown on every page; empty shows none.
	SiteBanner = "SITE_BANNER"
)

// Kind is the type of a setting's value.
type Kind string

const (
	KindBool   Kind = "bool"
	KindInt    Kind = "int"
	KindString Kind = "string"
)

// MaxStringRunes bounds a string setting unless its definition says less.
const MaxStringRunes = 500

// Definition describes one setting.
type Definition struct {
	Key  string `json:"key"`
	Kind Kind   `json:"kind"`
	// Default is the text form of the value used while no row exists.
	Default     string `json:"default"`
	Description string `json:"description"`
	// Min and Max bound an int setting.
	Min int64 `json:"min,omitempty"`
	Max int64 `json:"max,omitempty"`
	// MaxRunes bounds a string setting (MaxStringRunes when 0).
	MaxRunes int `json:"max_runes,omitempty"`
}

var definitions = []Definition{
	{
		Key: AllowUploads, Kind: KindBool, Default: "true",
		Description: "Accept new uploads, revisions, quick previews and publishes. Off pauses them for everyone.",
	},
	{
		Key: AllowAnonymousUploads, Kind: KindBool, Default: "true",
		Description: "Accept uploads, revisions and quick previews from signed-out visitors. Signed-in users keep uploading.",
	},
	{
		Key: AllowSignups, Kind: KindBool, Default: "true",
		Description: "Let new accounts be created (provider and passkey sign-ups). Existing accounts still sign in.",
	},
	{
		Key: AllowComments, Kind: KindBool, Default: "true",
		Description: "Accept new comments. Existing comments stay visible and removable.",
	},
	{
		Key: SiteBanner, Kind: KindString, Default: "",
		Description: "A notice shown at the top of every page (maintenance, render backlog). Empty shows none.",
		MaxRunes:    300,
	},
}

// Definitions lists every setting, in display order.
func Definitions() []Definition { return slices.Clone(definitions) }

// Lookup finds a setting's definition.
func Lookup(key string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Key == key {
			return definition, true
		}
	}
	return Definition{}, false
}

var (
	// ErrUnknownKey is a key no definition names.
	ErrUnknownKey = errors.New("unknown setting")
	// ErrInvalidValue is a value that does not fit its setting; the message
	// says why and is safe to show.
	ErrInvalidValue = errors.New("invalid setting value")
)

// Normalize checks raw against the definition and returns its canonical text
// form (what is stored): true/false, a decimal number, a trimmed string.
func (d Definition) Normalize(raw string) (string, error) {
	switch d.Kind {
	case KindBool:
		if raw != "true" && raw != "false" {
			return "", d.invalid("must be true or false")
		}
		return raw, nil
	case KindInt:
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", d.invalid("must be a whole number")
		}
		if number < d.Min || number > d.Max {
			return "", d.invalid("must be between %d and %d", d.Min, d.Max)
		}
		return strconv.FormatInt(number, 10), nil
	case KindString:
		value := strings.TrimSpace(raw)
		if !utf8.ValidString(value) || containsControl(value) {
			return "", d.invalid("must be plain text on one line")
		}
		limit := d.MaxRunes
		if limit == 0 {
			limit = MaxStringRunes
		}
		if utf8.RuneCountInString(value) > limit {
			return "", d.invalid("is at most %d characters", limit)
		}
		return value, nil
	default:
		return "", fmt.Errorf("setting %s has unknown kind %q", d.Key, d.Kind)
	}
}

func (d Definition) invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s %s", ErrInvalidValue, d.Key, fmt.Sprintf(format, args...))
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// Values are the effective settings: every defined key, stored or default.
type Values map[string]string

// Defaults is every setting at its default.
func Defaults() Values {
	values := make(Values, len(definitions))
	for _, definition := range definitions {
		values[definition.Key] = definition.Default
	}
	return values
}

// Merge overlays stored rows on the defaults. A row of an unknown key, or one
// whose value no longer fits its definition, is skipped (its default stays);
// skipped says which, for logging.
func Merge(stored map[string]string) (values Values, skipped []string) {
	values = Defaults()
	for key, raw := range stored {
		definition, ok := Lookup(key)
		if !ok {
			skipped = append(skipped, key)
			continue
		}
		value, err := definition.Normalize(raw)
		if err != nil {
			skipped = append(skipped, key)
			continue
		}
		values[key] = value
	}
	slices.Sort(skipped)
	return values, skipped
}

// Bool is a boolean setting; false for a key that is not one.
func (v Values) Bool(key string) bool { return v[key] == "true" }

// String is a setting's text.
func (v Values) String(key string) string { return v[key] }

// Int is a number setting; 0 for a key that is not one.
func (v Values) Int(key string) int64 {
	number, _ := strconv.ParseInt(v[key], 10, 64)
	return number
}
