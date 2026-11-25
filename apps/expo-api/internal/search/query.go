package search

import "time"

// Query is one GET /api/v1/search request, already parsed out of query
// parameters. Icons is populated from bracketed rich-text tokens found in the
// raw query text (see icons.go): a query of "[fluid=water] cooling tower"
// splits into Icons: ["[fluid=water]"] and Text: "cooling tower", so the icon
// tag is matched as an exact facet while the rest is matched as free text.
type Query struct {
	// Text is the free-text portion of the query, with any icon tokens
	// already removed. Empty means "match everything".
	Text string
	// Icons are exact rich-text icon tokens (e.g. "[fluid=water]") that a
	// matching entry's icons must all contain.
	Icons []string
	// Tags, Entities, and Recipes are OR-matched within each field: any
	// entry carrying at least one of the listed values in that field
	// matches.
	Tags []string
	// Mods are mod filter values (mods.Vanilla, mods.SpaceAge, modset
	// names), OR-matched like Tags.
	Mods          []string
	Entities      []string
	Recipes       []string
	EntityQuality string
	RecipeQuality string
	// GameVersion accepts an exact source game version or one of the public
	// release-family selectors (pre-2.0, v2.0, v2.1, v2.1-compatible).
	GameVersion string
	// Kinds are result kinds (blueprint, book, upgrade_planner,
	// deconstruction_planner), OR-matched; none means every kind.
	Kinds []string
	// FootprintWidth and FootprintHeight (tiles, 0 for unset) keep
	// blueprints that fit in that area, either way round.
	FootprintWidth  int
	FootprintHeight int

	PlatformsOnly   bool
	FactoriesOnly   bool
	PlatformFilters []PlatformFilter

	// ProductionFilters are numeric filters on one leaf's item rates,
	// combined with AND.
	ProductionFilters []ProductionFilter
	// CostFilters are numeric filters on one blueprint's raw build cost,
	// combined with AND.
	CostFilters []CostFilter
	// Spoiler is SpoilerExclude, SpoilerOnly, or empty for every entry.
	Spoiler string
	// CreatedAfter and CreatedBefore bound created_at (unix seconds,
	// inclusive); nil is unbounded.
	CreatedAfter  *int64
	CreatedBefore *int64
	// Facets asks for the facet counts of the browse sidebar.
	Facets bool
	// SimilarTo is a blueprint's id: results are the live public blueprints
	// like it (similar.go), closest production rate first; Sort is ignored.
	SimilarTo string

	Sort   string
	Limit  int32
	Offset int32
}

// PlatformFilter compares one metric of a platform leaf.
type PlatformFilter struct {
	Metric   string
	Operator string
	Value    float64
}

// The spoiler filter values.
const (
	SpoilerExclude = "exclude"
	SpoilerOnly    = "only"
)

// ProductionFilter narrows results using one item's per-second rate.
// Direction (ValidDirection) is "out" for the produced/output rate, "in" for
// the consumed/input rate, "out_per_tile" for the output rate per tile of the
// blueprint's footprint, or "out_per_mw" for the output rate per MW of
// electricity it consumes; the last two match blueprints only (a book has no
// one footprint or consumption). Item is a Factorio internal item/fluid name
// (e.g. "iron-gear-wheel"); a non-empty Quality restricts the comparison to
// that exact item tier.
type ProductionFilter struct {
	Operator  string
	Rate      float64
	Item      string
	Quality   string
	Direction string
}

// CostFilter narrows blueprints by the amount of one raw resource (an item
// or fluid name) their build cost breaks down to.
type CostFilter struct {
	Resource string
	Operator string
	Amount   float64
}

// Result is one page of search hits. KindCounts are the matches per kind
// with the kind filter left out, so a client can show each kind's count;
// Facets, set only for Query.Facets, are the other facet counts likewise.
type Result struct {
	Total      int64
	Hits       []Hit
	KindCounts map[string]int64
	Facets     *Facets
}

// Facets are the match counts of the browse sidebar's curated facets, each
// counted with its own filter left out and every other filter applied. A
// value with no match is absent.
type Facets struct {
	// Tags are the matches per tag slug.
	Tags map[string]int64
	// Platform counts the matches that are space platforms and those that
	// are not, under PlatformPlatforms and PlatformFactories.
	Platform map[string]int64
	// Mods are the matches per mod filter value (vanilla, space-age, modsets).
	Mods map[string]int64
	// Versions are the matches per game version family (versionGroups).
	Versions map[string]int64
}

// The keys of Facets.Platform, the blueprint_type filter's values.
const (
	PlatformFactories = "factories"
	PlatformPlatforms = "platforms"
)

// Hit is one matching result with the indexed text its card shows.
type Hit struct {
	EntryID string
	// Kind is blueprint, book or a planner kind; NodeID the blueprint's or
	// planner's id (empty for a book).
	Kind   string
	NodeID string
	// Title, Description and Icons are the card's: the node's own, or the
	// entry's for a book or an entry's only node.
	Title       string
	Description string
	// BookTitle is the book entry's title when the node is inside a book.
	BookTitle   string
	Icons       string
	Tags        []string
	GameVersion string
	// ModBadges are the badge titles for the mods the result needs.
	ModBadges []string

	CreatedAt time.Time
	UpdatedAt time.Time
}
