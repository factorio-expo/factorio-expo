package search

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// Facet limits: Typesense returns 10 values per facet unless asked, and a
// page of results is at most maxSearchLimit.
const (
	maxFacetValues = 250
	maxSearchLimit = 250
	facetKind      = "kind"
)

// facetSpec is one counted facet: the schema field, whether the query
// filters on it, and how to take that filter out. A facet is counted with its
// own filter left out (a visitor choosing kinds still sees every kind's
// count) and every other filter applied.
type facetSpec struct {
	field  string
	active func(Query) bool
	clear  func(*Query)
}

// facetSpecs are the facets the sidebar counts, kind always and the rest when
// Query.Facets is set.
var facetSpecs = []facetSpec{
	{
		field:  facetKind,
		active: func(q Query) bool { return len(q.Kinds) > 0 },
		clear:  func(q *Query) { q.Kinds = nil },
	},
	{
		field:  "tags",
		active: func(q Query) bool { return len(q.Tags) > 0 },
		clear:  func(q *Query) { q.Tags = nil },
	},
	{
		field:  "mods",
		active: func(q Query) bool { return len(q.Mods) > 0 },
		clear:  func(q *Query) { q.Mods = nil },
	},
	{
		field:  "version_groups",
		active: func(q Query) bool { return q.GameVersion != "" },
		clear:  func(q *Query) { q.GameVersion = "" },
	},
	{
		field:  "has_platform",
		active: func(q Query) bool { return q.FactoriesOnly || q.PlatformsOnly },
		clear: func(q *Query) {
			q.FactoriesOnly, q.PlatformsOnly = false, false
		},
	},
}

// splitFacets divides the counted facets into the fields the main request
// can facet on (their filter is not set, so its results are the right
// population) and the specs whose count needs a request of its own.
func splitFacets(query Query) (inline []string, separate []facetSpec) {
	for _, spec := range facetSpecs {
		if spec.field != facetKind && !query.Facets {
			continue
		}
		if spec.active(query) {
			separate = append(separate, spec)
		} else {
			inline = append(inline, spec.field)
		}
	}
	return inline, separate
}

// facetCounts counts one facet with its own filter left out: a facet-only
// request (no results) over the same query. A filter on a dynamic field no
// document has yields no counts.
func (c *TypesenseClient) facetCounts(
	ctx context.Context, collection string, query Query, spec facetSpec, dynamic []string,
) (map[string]int64, error) {
	spec.clear(&query)
	params := url.Values{}
	params.Set("q", queryText(query))
	params.Set("query_by", "search_text")
	if filter := buildFilter(query); filter != "" {
		params.Set("filter_by", filter)
	}
	params.Set("facet_by", spec.field)
	params.Set("max_facet_values", strconv.Itoa(maxFacetValues))
	params.Set("per_page", "0")
	response, empty, err := c.search(ctx, collection, params, dynamic)
	if err != nil || empty {
		return map[string]int64{}, err
	}
	return response.facetCounts(spec.field), nil
}

// facetsFrom shapes the counts by facet field into Facets; every map is
// non-nil.
func facetsFrom(counts map[string]map[string]int64) *Facets {
	facets := &Facets{
		Tags:     nonNilCounts(counts["tags"]),
		Mods:     nonNilCounts(counts["mods"]),
		Versions: nonNilCounts(counts["version_groups"]),
		Platform: map[string]int64{},
	}
	for value, count := range counts["has_platform"] {
		if strings.EqualFold(value, "true") {
			facets.Platform[PlatformPlatforms] = count
		} else {
			facets.Platform[PlatformFactories] = count
		}
	}
	return facets
}

func nonNilCounts(counts map[string]int64) map[string]int64 {
	if counts == nil {
		return map[string]int64{}
	}
	return counts
}
