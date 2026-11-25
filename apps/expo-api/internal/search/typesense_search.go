package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Search answers a parsed Query against the blueprints collection: every hit
// is one result (a blueprint, a planner or a top-level book). A result matches
// its own text only. KindCounts, and with Query.Facets the other facet counts,
// count the matches with the facet's own filter left out (typesense_facets.go).
// It reads the published version while this build's is being built, so a query
// that needs the new schema can fail until Rebuild publishes it.
func (c *TypesenseClient) Search(ctx context.Context, query Query) (Result, error) {
	if query.SimilarTo != "" {
		return c.searchSimilar(ctx, query)
	}
	names := c.queried.Load()
	sort, err := SortExpression(query.Sort)
	if err != nil {
		return Result{}, err
	}
	params := url.Values{}
	if sort != "" {
		params.Set("sort_by", sort)
	}
	params.Set("q", queryText(query))
	params.Set("query_by", "search_text")
	if filter := buildFilter(query); filter != "" {
		params.Set("filter_by", filter)
	}
	inline, separate := splitFacets(query)
	if len(inline) > 0 {
		params.Set("facet_by", strings.Join(inline, ","))
		params.Set("max_facet_values", strconv.Itoa(maxFacetValues))
	}

	limit := query.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, maxSearchLimit)
	offset := max(query.Offset, 0)
	// offset/limit, not page/per_page: a page index cannot express an offset
	// that is not a multiple of the limit.
	params.Set("limit", strconv.Itoa(int(limit)))
	params.Set("offset", strconv.Itoa(int(offset)))

	dynamic := dynamicFilterFields(query)
	response, empty, err := c.search(ctx, names.Blueprints, params, dynamic)
	if err != nil || empty {
		return emptyResult(query), err
	}
	counts := map[string]map[string]int64{}
	for _, field := range inline {
		counts[field] = response.facetCounts(field)
	}
	for _, spec := range separate {
		if counts[spec.field], err = c.facetCounts(ctx, names.Blueprints, query, spec, dynamic); err != nil {
			return Result{}, err
		}
	}

	hits, err := response.hits()
	if err != nil {
		return Result{}, err
	}
	result := Result{Total: response.Found, Hits: hits, KindCounts: counts[facetKind]}
	if query.Facets {
		result.Facets = facetsFrom(counts)
	}
	return result, nil
}

// queryText is the q parameter: the free text, or "*" for everything.
func queryText(query Query) string {
	if strings.TrimSpace(query.Text) == "" {
		return "*"
	}
	return query.Text
}

// emptyResult is the answer when a filter names a dynamic field no document
// has: nothing matches, so every count is zero.
func emptyResult(query Query) Result {
	result := Result{Hits: []Hit{}, KindCounts: map[string]int64{}}
	if query.Facets {
		result.Facets = facetsFrom(nil)
	}
	return result
}

// search runs one search request. empty is true when a production or cost
// filter names a dynamic field (of dynamic) no document carries yet: nothing
// can match.
func (c *TypesenseClient) search(
	ctx context.Context, collection string, params url.Values, dynamic []string,
) (response tsSearchResponse, empty bool, err error) {
	status, body, err := c.do(ctx, http.MethodGet, "/collections/"+collection+"/documents/search", params, nil)
	if err != nil {
		return response, false, err
	}
	if status != http.StatusOK {
		if missingDynamicField(status, body, dynamic) {
			return response, true, nil
		}
		return response, false, fmt.Errorf("search: %s", describeError(status, body))
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return response, false, fmt.Errorf("decode search response: %w", err)
	}
	return response, false, nil
}

// dynamicFilterFields are the dynamic fields the query's production and cost
// filters compare.
func dynamicFilterFields(query Query) []string {
	var fields []string
	for _, filter := range query.ProductionFilters {
		fields = append(fields, rateFieldName(filter.Direction, filter.Quality, filter.Item))
	}
	for _, filter := range query.CostFilters {
		fields = append(fields, costFieldName(filter.Resource))
	}
	return fields
}

func missingDynamicField(status int, body []byte, fields []string) bool {
	if status != http.StatusBadRequest && status != http.StatusNotFound {
		return false
	}
	var response struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return false
	}
	for _, field := range fields {
		// Dynamic fields only exist after a document carries them. Every
		// production and cost constraint is ANDed, so an absent field cannot
		// match.
		if response.Message == "Could not find a filter field named `"+field+"` in the schema." {
			return true
		}
	}
	return false
}

// tsSearchResponse and tsBlueprintDocument mirror Typesense's search
// response and the part of a blueprints document a Hit needs.
type tsSearchResponse struct {
	Found int64 `json:"found"`
	Hits  []struct {
		Document json.RawMessage `json:"document"`
	} `json:"hits"`
	FacetCounts []struct {
		FieldName string `json:"field_name"`
		Counts    []struct {
			Value string `json:"value"`
			Count int64  `json:"count"`
		} `json:"counts"`
	} `json:"facet_counts"`
}

// facetCounts are the response's matches per value of one facet field.
func (r tsSearchResponse) facetCounts(field string) map[string]int64 {
	counts := map[string]int64{}
	for _, facet := range r.FacetCounts {
		if facet.FieldName != field {
			continue
		}
		for _, count := range facet.Counts {
			counts[count.Value] = count.Count
		}
	}
	return counts
}

// hits decodes the response's documents.
func (r tsSearchResponse) hits() ([]Hit, error) {
	hits := make([]Hit, 0, len(r.Hits))
	for _, hit := range r.Hits {
		var doc tsBlueprintDocument
		if err := json.Unmarshal(hit.Document, &doc); err != nil {
			return nil, fmt.Errorf("decode search hit: %w", err)
		}
		hits = append(hits, doc.toHit())
	}
	return hits, nil
}

type tsBlueprintDocument struct {
	EntryID     string   `json:"entry_id"`
	Kind        string   `json:"kind"`
	NodeID      string   `json:"node_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	BookTitle   string   `json:"book_title"`
	IconString  string   `json:"icon_string"`
	Tags        []string `json:"tags"`
	GameVersion string   `json:"game_version"`
	ModBadges   []string `json:"mod_badges"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

func (d tsBlueprintDocument) toHit() Hit {
	return Hit{
		EntryID:     d.EntryID,
		Kind:        d.Kind,
		NodeID:      d.NodeID,
		Title:       d.Title,
		Description: d.Description,
		BookTitle:   d.BookTitle,
		Icons:       d.IconString,
		Tags:        d.Tags,
		GameVersion: d.GameVersion,
		ModBadges:   d.ModBadges,
		CreatedAt:   time.Unix(d.CreatedAt, 0).UTC(),
		UpdatedAt:   time.Unix(d.UpdatedAt, 0).UTC(),
	}
}
