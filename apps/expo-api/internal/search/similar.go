package search

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/fault"
)

// ErrSimilarNotFound is a similar_to id that is no live public blueprint: not
// in the index (unknown, private, expired, a quick preview, deleted, or not
// a blueprint).
var ErrSimilarNotFound = fault.New(fault.NotFound, "not_found", "blueprint not found")

// similarSeed is what "similar" compares: the blueprint's highest-rate output,
// that output's rate, and whether it is a space platform.
type similarSeed struct {
	product  string
	rate     float64
	platform bool
}

// searchSimilar answers a query with SimilarTo: the live public blueprints
// that make the same top output product as the seed blueprint and are, like
// it, either ground factories or space platforms, other than the seed itself.
// The query's other filters and its text still apply (its kind and sort do
// not: results are blueprints, closest production rate first).
//
// Closeness is how near the blueprint's rate of that product is to the
// seed's, on a ratio scale (twice and half the rate are equally far:
// |ln(rate / seed rate)|). Typesense cannot sort by distance from a value, so
// two requests read the blueprints at or above the seed's rate (rate
// ascending) and those below it (descending), each the first offset+limit,
// and the nearest are merged. At most the maxSearchLimit nearest can be
// reached, so offset+limit beyond it lists what is there. Blueprints tie by
// newest first. Total counts every similar blueprint; there are no facet
// counts.
func (c *TypesenseClient) searchSimilar(ctx context.Context, query Query) (Result, error) {
	names := c.queried.Load()
	seed, err := c.similarSeed(ctx, names.Blueprints, query.SimilarTo)
	if err != nil {
		return Result{}, err
	}
	empty := Result{Hits: []Hit{}, KindCounts: map[string]int64{}}
	if seed.product == "" || seed.rate <= 0 {
		return empty, nil
	}

	query.Kinds = []string{string(blueprint.KindBlueprint)}
	var filter strings.Builder
	filter.WriteString(buildFilter(query))
	rate := rateFieldName(DirectionOut, "", seed.product)
	for _, clause := range []string{
		"top_product:=" + filterValue(seed.product),
		"has_platform:=" + strconv.FormatBool(seed.platform),
		"id:!=" + filterValue(query.SimilarTo),
	} {
		filter.WriteString(" && " + clause)
	}
	limit := query.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	offset := max(int(query.Offset), 0)
	window := min(offset+int(limit), maxSearchLimit)
	origin := formatFloat(seed.rate)

	read := func(comparison, order string) (similarSide, error) {
		params := url.Values{}
		params.Set("q", queryText(query))
		params.Set("query_by", "search_text")
		params.Set("filter_by", filter.String()+" && "+rate+":"+comparison+origin)
		params.Set("sort_by", rate+":"+order+",created_at:desc,position:asc")
		params.Set("per_page", strconv.Itoa(window))
		response, missing, err := c.search(ctx, names.Blueprints, params, []string{rate})
		if err != nil || missing {
			return similarSide{}, err
		}
		return readSimilarSide(response, rate, seed.rate)
	}
	above, err := read(">=", "asc")
	if err != nil {
		return Result{}, err
	}
	below, err := read("<", "desc")
	if err != nil {
		return Result{}, err
	}

	merged := mergeSimilar(above, below, window)
	total := above.found + below.found
	if offset >= len(merged) {
		merged = nil
	} else {
		merged = merged[offset:min(offset+int(limit), len(merged))]
	}
	hits := make([]Hit, 0, len(merged))
	for _, candidate := range merged {
		hits = append(hits, candidate.hit)
	}
	return Result{
		Total: total, Hits: hits,
		KindCounts: map[string]int64{string(blueprint.KindBlueprint): total},
	}, nil
}

// similarSeed reads the seed blueprint's facts from its document.
func (c *TypesenseClient) similarSeed(ctx context.Context, collection, id string) (similarSeed, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/collections/"+collection+"/documents/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return similarSeed{}, err
	}
	if status == http.StatusNotFound {
		return similarSeed{}, ErrSimilarNotFound
	}
	if status != http.StatusOK {
		return similarSeed{}, fmt.Errorf("read similar_to document: %s", describeError(status, body))
	}
	var doc struct {
		Kind        string `json:"kind"`
		TopProduct  string `json:"top_product"`
		HasPlatform bool   `json:"has_platform"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return similarSeed{}, fmt.Errorf("decode similar_to document: %w", err)
	}
	if doc.Kind != string(blueprint.KindBlueprint) {
		return similarSeed{}, ErrSimilarNotFound
	}
	seed := similarSeed{product: doc.TopProduct, platform: doc.HasPlatform}
	if doc.TopProduct != "" {
		var rates map[string]json.RawMessage
		if err := json.Unmarshal(body, &rates); err != nil {
			return similarSeed{}, fmt.Errorf("decode similar_to rates: %w", err)
		}
		if raw, ok := rates[rateFieldName(DirectionOut, "", doc.TopProduct)]; ok {
			if err := json.Unmarshal(raw, &seed.rate); err != nil {
				return similarSeed{}, fmt.Errorf("decode similar_to rate: %w", err)
			}
		}
	}
	return seed, nil
}

// similarCandidate is a hit with its distance from the seed's rate.
type similarCandidate struct {
	hit      Hit
	distance float64
}

// similarSide is one side of the seed's rate, nearest first.
type similarSide struct {
	found      int64
	candidates []similarCandidate
}

func readSimilarSide(response tsSearchResponse, rateField string, origin float64) (similarSide, error) {
	side := similarSide{found: response.Found}
	for _, hit := range response.Hits {
		var doc tsBlueprintDocument
		if err := json.Unmarshal(hit.Document, &doc); err != nil {
			return similarSide{}, fmt.Errorf("decode search hit: %w", err)
		}
		var rates map[string]json.RawMessage
		if err := json.Unmarshal(hit.Document, &rates); err != nil {
			return similarSide{}, fmt.Errorf("decode search hit rates: %w", err)
		}
		var rate float64
		if err := json.Unmarshal(rates[rateField], &rate); err != nil || rate <= 0 {
			return similarSide{}, fmt.Errorf("search hit without a %s rate", rateField)
		}
		side.candidates = append(side.candidates, similarCandidate{
			hit: doc.toHit(), distance: math.Abs(math.Log(rate / origin)),
		})
	}
	return side, nil
}

// mergeSimilar takes the count nearest candidates of two sides that are each
// ordered nearest first.
func mergeSimilar(a, b similarSide, count int) []similarCandidate {
	merged := make([]similarCandidate, 0, min(count, len(a.candidates)+len(b.candidates)))
	i, j := 0, 0
	for len(merged) < count && (i < len(a.candidates) || j < len(b.candidates)) {
		switch {
		case j >= len(b.candidates) || (i < len(a.candidates) && a.candidates[i].distance <= b.candidates[j].distance):
			merged = append(merged, a.candidates[i])
			i++
		default:
			merged = append(merged, b.candidates[j])
			j++
		}
	}
	return merged
}
