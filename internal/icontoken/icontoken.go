// Package icontoken is the Go side of the rich-text icon grammar:
// "[item=iron-plate]" or "[item=iron-plate,quality=rare]". The web twin is
// apps/expo-web/src/lib/icon-token.ts; both are tested against the shared
// fixture test-data/icon-tokens.json.
//
// Canonical form: "normal" quality is omitted (Format keeps it only on request,
// for search filters that must tell an explicit "normal" from "any"). Fluids
// have no quality in the game: a fluid token parses as quality-less whatever it
// spells out, and is formatted without one.
package icontoken

import (
	"regexp"
	"slices"
	"strings"
)

// Qualities lists the valid quality names, lowest first.
var Qualities = []string{"normal", "uncommon", "rare", "epic", "legendary"}

// iconTypes are the tag types that render as icons. Other tags such as
// [color=...] or [font=...] are not icons.
var iconTypes = map[string]bool{
	"item": true, "entity": true, "recipe": true, "fluid": true, "virtual-signal": true,
	"planet": true, "space-location": true, "quality": true, "tile": true,
	"technology": true, "achievement": true, "item-group": true,
}

// IconTypes returns the icon tag types (for tests).
func IconTypes() []string {
	types := make([]string, 0, len(iconTypes))
	for name := range iconTypes {
		types = append(types, name)
	}
	return types
}

// ValidQuality reports whether quality is empty (unspecified) or a known name.
func ValidQuality(quality string) bool {
	if quality == "" {
		return true
	}
	return slices.Contains(Qualities, quality)
}

// Token is one icon tag. Quality is "normal" when the tag carries none, and
// always for fluids.
type Token struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Quality string `json:"quality"`
}

// Scanned is a Token found in text, with the tag exactly as written.
type Scanned struct {
	Token
	Raw string `json:"raw"`
}

var pattern = regexp.MustCompile(`\[([a-zA-Z0-9-]+)=([a-zA-Z0-9_-]+)(?:,quality=([a-zA-Z0-9_-]+))?]`)

func fromMatch(match []string) (Token, bool) {
	if !iconTypes[match[1]] {
		return Token{}, false
	}
	quality := match[3]
	if quality == "" || match[1] == "fluid" {
		quality = "normal"
	}
	return Token{Type: match[1], Name: match[2], Quality: quality}, true
}

// Parse parses text as exactly one icon token.
func Parse(text string) (Token, bool) {
	match := pattern.FindStringSubmatch(text)
	if match == nil || match[0] != text {
		return Token{}, false
	}
	return fromMatch(match)
}

// Format renders a token, omitting "normal" quality unless keepNormal is set.
// A fluid token never carries a quality.
func Format(token Token, keepNormal bool) string {
	var out strings.Builder
	out.WriteString("[" + token.Type + "=" + token.Name)
	if token.Type != "fluid" && token.Quality != "" && (token.Quality != "normal" || keepNormal) {
		out.WriteString(",quality=" + token.Quality)
	}
	out.WriteByte(']')
	return out.String()
}

// Scan returns every icon token in text, in order.
func Scan(text string) []Scanned {
	found := []Scanned{}
	for _, match := range pattern.FindAllStringSubmatch(text, -1) {
		if token, ok := fromMatch(match); ok {
			found = append(found, Scanned{Token: token, Raw: match[0]})
		}
	}
	return found
}

// Strip returns text without its icon tokens; other text is untouched.
func Strip(text string) string {
	return pattern.ReplaceAllStringFunc(text, func(raw string) string {
		if _, ok := Parse(raw); ok {
			return ""
		}
		return raw
	})
}

// Extract splits text into its icon tokens (as written, except that a fluid
// token loses any quality it spells out) and the remaining free text, with the
// whitespace the tokens leave behind collapsed. Other bracketed tags such as
// [color=red] stay in the free text. Search indexes and matches these tokens,
// so "[fluid=water,quality=rare]" finds what "[fluid=water]" finds.
func Extract(text string) (tokens []string, remainder string) {
	for _, scanned := range Scan(text) {
		token := scanned.Raw
		if scanned.Type == "fluid" {
			token = Format(scanned.Token, false)
		}
		tokens = append(tokens, token)
	}
	return tokens, strings.Join(strings.Fields(Strip(text)), " ")
}
