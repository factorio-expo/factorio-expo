package search

import (
	"regexp"

	"factorio-expo/internal/icontoken"
)

// splitIconTokens breaks a stored rich-text icon string (e.g.
// "[item=iron-plate][fluid=water,quality=rare]") into its individual bracket
// tokens, unmodified. Only icon tokens count (internal/icontoken); other tags
// such as [color=red] are not indexed. Each tag is indexed (and later matched)
// as one opaque, exact facet value. The same scanner is reused to pull icon tokens back out of a user's search query, so
// a query like "[fluid=water] cooling tower" is split into the icon token
// "[fluid=water]" plus the free-text remainder "cooling tower" using exactly
// the same rule that produced the stored tokens.
func splitIconTokens(s string) []string {
	tokens, _ := extractIconTokens(s)
	return tokens
}

// ParseQueryText splits a raw search box query into its icon tokens and the
// remaining free text, e.g. "[fluid=water] cooling tower" ->
// (["[fluid=water]"], "cooling tower"). httpapi uses this to build a Query
// from the single "q" parameter before calling Client.Search.
func ParseQueryText(raw string) (icons []string, text string) {
	return extractIconTokens(raw)
}

// stripEmbeddedIcons splits display text (a label or description) into its
// free-text remainder and any bracketed icon tokens embedded directly in it.
// Factorio blueprint labels commonly embed rich-text icons inline, e.g.
// "[item=agricultural-science-pack]Gleba science [item=turbo-transport-belt]+".
// If those tokens were left in the stored title/description field, Typesense
// would tokenize "pack]Gleba" as one opaque chunk (no whitespace separates
// the closing bracket from the following word), making "gleba" unsearchable
// as free text. Stripping them keeps the field's free-text tokens clean and
// returns the tokens so callers can fold them into the document's icons
// facet - the same field an icon-only query like "[item=...]" matches
// against - so an icon embedded in a label is still findable either way.
// Formatting tags ([color=red]...[/color], [font=...]...[/font]) and the
// [space-age] logo are dropped for the same reason and are not indexed.
func stripEmbeddedIcons(s string) (text string, icons []string) {
	icons, text = extractIconTokens(formattingTags.ReplaceAllString(s, " "))
	return text, icons
}

var formattingTags = regexp.MustCompile(`\[(?:color|font)=[^\][]*\]|\[/(?:color|font)\]|\[space-age\]`)

// mergeIconLists concatenates any number of icon-token lists, preserving
// first-seen order and dropping duplicates (the same token can appear in a
// document's dedicated icon field and again embedded in its label).
func mergeIconLists(lists ...[]string) []string {
	seen := make(map[string]struct{})
	var merged []string
	for _, list := range lists {
		for _, icon := range list {
			if _, ok := seen[icon]; ok {
				continue
			}
			seen[icon] = struct{}{}
			merged = append(merged, icon)
		}
	}
	return merged
}

// extractIconTokens returns the icon tokens in s, unmodified, alongside the
// remaining text with those tokens (and the whitespace they leave behind)
// removed. Non-icon tags such as [color=red] and unclosed brackets stay in the
// remainder as ordinary text.
func extractIconTokens(s string) (tokens []string, remainder string) {
	return icontoken.Extract(s)
}
