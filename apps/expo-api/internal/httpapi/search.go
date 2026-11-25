package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/search"
)

// searchResponse is the stable contract the frontend builds against.
// KindCounts are the matches per result kind with the kind filter left out.
type searchResponse struct {
	Query      string           `json:"query" doc:"The query as understood."`
	Limit      int32            `json:"limit" doc:"The page size used."`
	Offset     int32            `json:"offset" doc:"How many results were skipped."`
	Total      int64            `json:"total" doc:"How many results match."`
	KindCounts map[string]int64 `json:"kind_counts" doc:"The matches per result kind with the kind filter left out."`
	// FacetCounts is set only for facets=true.
	FacetCounts *searchFacetCounts         `json:"facet_counts,omitempty" doc:"The match counts of the browse filters; only with facets=true, and not with similar_to."`
	Results     []catalog.SearchResultCard `json:"results" doc:"The result cards of this page."`
}

// searchFacetCounts are the counts of the sidebar's curated facets. Each
// counts the matches with its own filter left out and every other filter
// applied; a value with no match is absent.
type searchFacetCounts struct {
	Tags     map[string]int64 `json:"tags" doc:"The matches per tag slug, with the tags filter left out."`
	Platform map[string]int64 `json:"platform" doc:"The matches that are space platforms (platforms) and those that are not (factories), with blueprint_type left out."`
	Mods     map[string]int64 `json:"mods" doc:"The matches per mods filter value (vanilla, space-age, modset names), with the mods filter left out."`
	Versions map[string]int64 `json:"versions" doc:"The matches per game_version family (pre-2.0, v2.0, v2.1, v2.1-compatible), with the game_version filter left out."`
}

// searchEntries answers GET /api/v1/search against the Typesense-backed catalog
// projection (see docs/search.md): one result per matching blueprint,
// planner or top-level book. search.ParseQuery reads and validates every
// parameter (its filter table).
func (s *server) searchEntries(ctx context.Context, in *searchInput) (*searchOutput, error) {
	query, err := search.ParseQuery(in.values)
	if err != nil {
		return nil, err
	}
	result, err := s.search.Search(ctx, query)
	if errors.Is(err, search.ErrSimilarNotFound) {
		return nil, err
	}
	if err != nil {
		slog.Error("search entries", "error", err)
		return nil, errSearchUnavailable
	}

	nodes := make([]catalog.CardNode, 0, len(result.Hits))
	for _, hit := range result.Hits {
		nodes = append(nodes, catalog.CardNode{
			EntryID: hit.EntryID, NodeID: hit.NodeID, Kind: blueprint.Kind(hit.Kind),
			Title: hit.Title, Description: hit.Description, BookTitle: hit.BookTitle, Icons: hit.Icons,
			GameVersion: hit.GameVersion, Tags: hit.Tags, ModBadges: hit.ModBadges,
			CreatedAt: hit.CreatedAt, UpdatedAt: hit.UpdatedAt,
		})
	}
	// The index is never trusted to decide who may see an entry.
	cards, err := s.catalog.NodeCards(ctx, nodes, viewerFrom(ctx))
	if err != nil {
		slog.Error("build search result cards", "error", err)
		return nil, errSearchUnavailable
	}

	if result.KindCounts == nil {
		result.KindCounts = map[string]int64{}
	}
	body := searchResponse{
		Query: in.Q, Limit: query.Limit, Offset: query.Offset,
		Total: result.Total, KindCounts: result.KindCounts, Results: cards,
	}
	if facets := result.Facets; facets != nil {
		body.FacetCounts = &searchFacetCounts{
			Tags: facets.Tags, Platform: facets.Platform, Mods: facets.Mods, Versions: facets.Versions,
		}
	}
	return &searchOutput{Body: body}, nil
}

var errSearchUnavailable = huma.Error502BadGateway("search is temporarily unavailable")

type searchOutput struct {
	Body searchResponse
}

// searchInput documents and type-checks the search parameters;
// search.ParseQuery (the filter table) reads them from the raw query.
// TestOpenAPISpecMatchesRoutes keeps the two lists equal.
type searchInput struct {
	Q                 string `query:"q" doc:"Free text; icon tokens such as [item=iron-plate] must all be in the result's icons."`
	Tags              string `query:"tags" doc:"Comma-separated curated tag slugs (see /api/v1/tags)."`
	Mods              string `query:"mods" doc:"Comma-separated: vanilla, space-age or a modset name from /api/v1/modsets. OR-matched."`
	Entities          string `query:"entities" doc:"Comma-separated entity prototype names the blueprint must contain."`
	EntityQuality     string `query:"entity_quality" enum:"normal,uncommon,rare,epic,legendary" doc:"The quality the entities must have."`
	Recipes           string `query:"recipes" doc:"Comma-separated recipe names the blueprint must use."`
	RecipeQuality     string `query:"recipe_quality" enum:"normal,uncommon,rare,epic,legendary" doc:"The quality the recipes must have."`
	GameVersion       string `query:"game_version" doc:"The version that exported the blueprint, e.g. 2.1.24, or a release family: pre-2.0 (0.x and 1.x), v2.0, v2.1, v2.1-compatible (made in 2.1, or older and compatible with it)."`
	Kind              string `query:"kind" doc:"Comma-separated result kinds: blueprint, book, upgrade_planner, deconstruction_planner."`
	FootprintWidth    int    `query:"footprint_width" minimum:"0" doc:"With footprint_height: the blueprint fits in this many tiles (either way round)."`
	FootprintHeight   int    `query:"footprint_height" minimum:"0" doc:"With footprint_width."`
	BlueprintType     string `query:"blueprint_type" enum:"all,factories,platforms" doc:"Factories (no space platforms), space platforms only, or all."`
	PlatformFilters   string `query:"platform_filters" doc:"JSON array of {metric: weight_tons|thrust_mn|max_speed_km_s, operator: >|=|<, value}."`
	ProductionFilters string `query:"production_filters" doc:"JSON array of {type: item|fluid, product, quality, direction: in|out|out_per_tile|out_per_mw, operator: >|=|<, rate} (rate per second; out_per_tile divides the output rate by the blueprint's footprint area in tiles, out_per_mw by the electricity it consumes in MW; both match blueprints only)."`
	CostFilters       string `query:"cost_filters" doc:"JSON array (at most 8) of {type: item|fluid, resource, operator: >|=|<, amount}: the raw resources a blueprint's build cost breaks down to. Matches blueprints only."`
	Spoiler           string `query:"spoiler" enum:"exclude,only" doc:"Leave out, or keep only, entries marked spoiler/NSFW. Absent: every entry."`
	CreatedAfter      string `query:"created_after" doc:"Only entries created at or after this: a date (2026-11-14, from the start of that day UTC) or an RFC 3339 timestamp."`
	CreatedBefore     string `query:"created_before" doc:"Only entries created at or before this: a date (2026-11-14, through the end of that day UTC) or an RFC 3339 timestamp."`
	Facets            bool   `query:"facets" doc:"Also return facet_counts: the match counts of the tag, space platform, mods and game version filters, each with its own filter left out."`
	SimilarTo         string `query:"similar_to" doc:"A blueprint's id (the node_id of a result): list the live public blueprints like it instead, other than itself. They make the same top output product, are both ground factories or both space platforms, and come closest production rate first (sort is ignored, kind is blueprint, facet_counts are not returned); q and the other filters still apply. At most the 250 closest can be paged to. An id that is not a live public blueprint is a 404."`
	Sort              string `query:"sort" enum:"relevance,created_at:asc,created_at:desc,updated_at:asc,updated_at:desc,entity_count:asc,entity_count:desc,leaf_count:asc,leaf_count:desc,view_count:asc,view_count:desc" doc:"Default: newest update first."`
	Limit             int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
	Offset            int    `query:"offset" minimum:"0" default:"0"`

	values url.Values
}

// Resolve keeps the raw query for search.ParseQuery.
func (in *searchInput) Resolve(ctx huma.Context) []error {
	u := ctx.URL()
	in.values = u.Query()
	return nil
}

// registerSearch registers this file's operations.
func (s *server) registerSearch(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "searchEntries", Method: http.MethodGet, Path: "/search",
		Tags:    []string{readAPI, "Search"},
		Summary: "Search blueprints, planners and books",
		Description: "One result per matching blueprint, planner or top-level book, ranked by text " +
			"relevance or sorted. Filters work on the analysed contents of a blueprint, not on its title.",
		Errors: []int{http.StatusBadRequest, http.StatusNotFound, http.StatusBadGateway},
	}, accessOptional, limitSearch), s.searchEntries)
}
