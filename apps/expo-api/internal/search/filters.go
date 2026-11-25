package search

import (
	"encoding/json"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/internal/icontoken"
	"factorio-expo/internal/shortid"
)

// ErrInvalidQuery is a search request with a parameter ParseQuery refuses;
// its message names which.
var ErrInvalidQuery = fault.New(fault.Invalid, "invalid_search", "invalid search query")

// Paging bounds of a search request.
const (
	DefaultLimit = 20
	MaxLimit     = 100
	maxOffset    = 1 << 20
	// maxNumericFilters bounds each list of numeric filters (platform,
	// production, cost).
	maxNumericFilters = 8
	// maxFootprint is larger than any blueprint the upload limits allow.
	maxFootprint = 1 << 20
)

// platformMetrics are the platform filter metrics; each is a schema field of
// the same name.
var platformMetrics = []string{"weight_tons", "thrust_mn", "max_speed_km_s"}

// filter is one search filter: the query parameters it reads, how it parses
// them into the Query (the only place they are validated), the
// schemaBlueprints fields its clauses compare (wildcards as declared there)
// and the clauses it adds to filter_by. ParseQuery, buildFilter, Parameters
// and the schema test all read the same table, so a filter is one row.
type filter struct {
	params  []string
	parse   func(values url.Values, query *Query) error
	fields  []string
	clauses func(query Query) []string
}

// filters are in clause order. blueprint_type comes before the numeric
// filters, whose parse drops what the type rules out.
var filters = []filter{
	{
		// q is free text plus rich-text icon tokens; every icon must match.
		params: []string{"q"},
		parse: func(values url.Values, query *Query) error {
			query.Icons, query.Text = ParseQueryText(values.Get("q"))
			return nil
		},
		fields: []string{"icons"},
		clauses: func(query Query) []string {
			clauses := make([]string, 0, len(query.Icons))
			for _, icon := range query.Icons {
				clauses = append(clauses, "icons:="+filterValue(icon))
			}
			return clauses
		},
	},
	listFilter("tags", "tags", func(q *Query) *[]string { return &q.Tags }),
	listFilter("mods", "mods", func(q *Query) *[]string { return &q.Mods }),
	{
		// entities, narrowed to one quality tier by entity_quality.
		params: []string{"entities", "entity_quality"},
		parse: func(values url.Values, query *Query) error {
			query.Entities = splitList(values.Get("entities"))
			query.EntityQuality = values.Get("entity_quality")
			if !icontoken.ValidQuality(query.EntityQuality) {
				return ErrInvalidQuery.Withf("quality filters contain an unknown quality")
			}
			return nil
		},
		fields: []string{"entity_names", "entity_facts"},
		clauses: func(query Query) []string {
			return qualityClause(query.Entities, query.EntityQuality, "entity_names", "entity_facts")
		},
	},
	{
		params: []string{"recipes", "recipe_quality"},
		parse: func(values url.Values, query *Query) error {
			query.Recipes = splitList(values.Get("recipes"))
			query.RecipeQuality = values.Get("recipe_quality")
			if !icontoken.ValidQuality(query.RecipeQuality) {
				return ErrInvalidQuery.Withf("quality filters contain an unknown quality")
			}
			return nil
		},
		fields: []string{"recipes", "recipe_facts"},
		clauses: func(query Query) []string {
			return qualityClause(query.Recipes, query.RecipeQuality, "recipes", "recipe_facts")
		},
	},
	{
		params: []string{"game_version"},
		parse: func(values url.Values, query *Query) error {
			query.GameVersion = values.Get("game_version")
			return nil
		},
		fields: []string{"game_version", "version_groups"},
		clauses: func(query Query) []string {
			if query.GameVersion == "" {
				return nil
			}
			return []string{buildGameVersionFilter(query.GameVersion)}
		},
	},
	{
		params: []string{"kind"},
		parse: func(values url.Values, query *Query) error {
			query.Kinds = splitList(values.Get("kind"))
			for _, kind := range query.Kinds {
				if !blueprint.Kind(kind).IsPlanner() && kind != string(blueprint.KindBook) &&
					kind != string(blueprint.KindBlueprint) {
					return ErrInvalidQuery.Withf("kind must be blueprint, book, upgrade_planner or deconstruction_planner")
				}
			}
			return nil
		},
		fields: []string{"kind"},
		clauses: func(query Query) []string {
			if len(query.Kinds) == 0 {
				return nil
			}
			return []string{"kind:=" + filterValueList(query.Kinds)}
		},
	},
	{
		// Keeps blueprints that fit in width x height, either way round.
		params: []string{"footprint_width", "footprint_height"},
		parse: func(values url.Values, query *Query) error {
			query.FootprintWidth = boundedInt(values.Get("footprint_width"), 0, 0, maxFootprint)
			query.FootprintHeight = boundedInt(values.Get("footprint_height"), 0, 0, maxFootprint)
			return nil
		},
		fields:  []string{"footprint_short", "footprint_long"},
		clauses: func(query Query) []string { return buildFootprintFilter(query.FootprintWidth, query.FootprintHeight) },
	},
	{
		// all, factories or platforms; any platform filter also asks for a
		// platform (every document is one leaf, so its metrics all match it).
		params: []string{"blueprint_type"},
		parse: func(values url.Values, query *Query) error {
			switch values.Get("blueprint_type") {
			case "", "all":
			case "factories":
				query.FactoriesOnly = true
			case "platforms":
				query.PlatformsOnly = true
			default:
				return ErrInvalidQuery.Withf("blueprint_type must be all, factories, or platforms")
			}
			return nil
		},
		fields: []string{"has_platform"},
		clauses: func(query Query) []string {
			switch {
			case query.FactoriesOnly:
				return []string{"has_platform:=false"}
			case query.PlatformsOnly || len(query.PlatformFilters) > 0:
				return []string{"has_platform:=true"}
			}
			return nil
		},
	},
	{
		// A JSON array of {metric, operator, value}; factories have none.
		params: []string{"platform_filters"},
		parse: func(values url.Values, query *Query) error {
			var requested []struct {
				Metric   string   `json:"metric"`
				Operator string   `json:"operator"`
				Value    *float64 `json:"value"`
			}
			if err := parseFilterList(values, "platform_filters", &requested); err != nil {
				return err
			}
			for _, filter := range requested {
				if !slices.Contains(platformMetrics, filter.Metric) {
					return ErrInvalidQuery.Withf("platform_filters contains an unknown metric")
				}
				if !validComparison(filter.Operator, filter.Value) {
					return ErrInvalidQuery.Withf("platform_filters contains an invalid filter")
				}
				if !query.FactoriesOnly {
					query.PlatformFilters = append(query.PlatformFilters, PlatformFilter{
						Metric: filter.Metric, Operator: filter.Operator, Value: *filter.Value,
					})
				}
			}
			return nil
		},
		fields: platformMetrics,
		clauses: func(query Query) []string {
			clauses := make([]string, 0, len(query.PlatformFilters))
			for _, filter := range query.PlatformFilters {
				clauses = append(clauses, compareClause(filter.Metric, filter.Operator, filter.Value))
			}
			return clauses
		},
	},
	{
		// A JSON array of {type, product, quality, direction, operator,
		// rate}, combined with AND; platforms have no production.
		params: []string{"production_filters"},
		parse: func(values url.Values, query *Query) error {
			var requested []struct {
				Type      string   `json:"type"`
				Product   string   `json:"product"`
				Quality   string   `json:"quality"`
				Direction string   `json:"direction"`
				Operator  string   `json:"operator"`
				Rate      *float64 `json:"rate"`
			}
			if err := parseFilterList(values, "production_filters", &requested); err != nil {
				return err
			}
			for _, filter := range requested {
				product := strings.TrimSpace(filter.Product)
				if product == "" || !validMaterial(filter.Type) ||
					!icontoken.ValidQuality(filter.Quality) ||
					// Fluids have no quality, so a fluid filter with one would match nothing.
					(filter.Type == "fluid" && filter.Quality != "") ||
					!ValidDirection(filter.Direction) || !validComparison(filter.Operator, filter.Rate) {
					return ErrInvalidQuery.Withf("production_filters contains an invalid filter")
				}
				if !query.PlatformsOnly {
					query.ProductionFilters = append(query.ProductionFilters, ProductionFilter{
						Item: product, Direction: filter.Direction, Quality: filter.Quality,
						Operator: filter.Operator, Rate: *filter.Rate,
					})
				}
			}
			return nil
		},
		fields: []string{"rate_out_.*", "rate_in_.*", "rate_tile_out_.*", "rate_mw_out_.*"},
		clauses: func(query Query) []string {
			clauses := make([]string, 0, len(query.ProductionFilters))
			for _, filter := range query.ProductionFilters {
				field := rateFieldName(filter.Direction, filter.Quality, filter.Item)
				clauses = append(clauses, compareClause(field, filter.Operator, filter.Rate))
			}
			return clauses
		},
	},
	{
		// A JSON array of {type, resource, operator, amount}: raw resources
		// (item or fluid) a blueprint's build cost breaks down to.
		params: []string{"cost_filters"},
		parse: func(values url.Values, query *Query) error {
			var requested []struct {
				Type     string   `json:"type"`
				Resource string   `json:"resource"`
				Operator string   `json:"operator"`
				Amount   *float64 `json:"amount"`
			}
			if err := parseFilterList(values, "cost_filters", &requested); err != nil {
				return err
			}
			for _, filter := range requested {
				resource := strings.TrimSpace(filter.Resource)
				if resource == "" || !validMaterial(filter.Type) || !validComparison(filter.Operator, filter.Amount) {
					return ErrInvalidQuery.Withf("cost_filters contains an invalid filter")
				}
				query.CostFilters = append(query.CostFilters, CostFilter{
					Resource: resource, Operator: filter.Operator, Amount: *filter.Amount,
				})
			}
			return nil
		},
		fields: []string{"cost_raw_.*"},
		clauses: func(query Query) []string {
			clauses := make([]string, 0, len(query.CostFilters))
			for _, filter := range query.CostFilters {
				clauses = append(clauses, compareClause(costFieldName(filter.Resource), filter.Operator, filter.Amount))
			}
			return clauses
		},
	},
	{
		params: []string{"spoiler"},
		parse: func(values url.Values, query *Query) error {
			query.Spoiler = values.Get("spoiler")
			if query.Spoiler != "" && query.Spoiler != SpoilerExclude && query.Spoiler != SpoilerOnly {
				return ErrInvalidQuery.Withf("spoiler must be exclude or only")
			}
			return nil
		},
		fields: []string{"spoiler"},
		clauses: func(query Query) []string {
			switch query.Spoiler {
			case SpoilerExclude:
				return []string{"spoiler:=false"}
			case SpoilerOnly:
				return []string{"spoiler:=true"}
			}
			return nil
		},
	},
	{
		// created_at range; a bare date is the whole UTC day, so created_before
		// includes it.
		params: []string{"created_after", "created_before"},
		parse: func(values url.Values, query *Query) error {
			var err error
			if query.CreatedAfter, err = parseCreatedBound("created_after", values.Get("created_after"), false); err != nil {
				return err
			}
			query.CreatedBefore, err = parseCreatedBound("created_before", values.Get("created_before"), true)
			return err
		},
		fields: []string{"created_at"},
		clauses: func(query Query) []string {
			var clauses []string
			if query.CreatedAfter != nil {
				clauses = append(clauses, "created_at:>="+strconv.FormatInt(*query.CreatedAfter, 10))
			}
			if query.CreatedBefore != nil {
				clauses = append(clauses, "created_at:<="+strconv.FormatInt(*query.CreatedBefore, 10))
			}
			return clauses
		},
	},
	{
		// Facet counts for the browse sidebar; no clause.
		params: []string{"facets"},
		parse: func(values url.Values, query *Query) error {
			switch values.Get("facets") {
			case "", "false":
			case "true":
				query.Facets = true
			default:
				return ErrInvalidQuery.Withf("facets must be true or false")
			}
			return nil
		},
		clauses: func(Query) []string { return nil },
	},
	{
		// A blueprint's id: the search turns its facts into clauses
		// (similar.go), so there are none here.
		params: []string{"similar_to"},
		parse: func(values url.Values, query *Query) error {
			query.SimilarTo = values.Get("similar_to")
			if query.SimilarTo != "" && !shortid.Valid(query.SimilarTo) {
				return ErrInvalidQuery.Withf("similar_to must be a blueprint id")
			}
			return nil
		},
		clauses: func(Query) []string { return nil },
	},
	{
		// Order and paging; no clause.
		params: []string{"sort", "limit", "offset"},
		parse: func(values url.Values, query *Query) error {
			query.Sort = values.Get("sort")
			if _, err := SortExpression(query.Sort); err != nil {
				return ErrInvalidQuery.Withf("%s", err.Error())
			}
			query.Limit = int32(boundedInt(values.Get("limit"), DefaultLimit, 1, MaxLimit))
			query.Offset = int32(boundedInt(values.Get("offset"), 0, 0, maxOffset))
			return nil
		},
		clauses: func(Query) []string { return nil },
	},
}

// ParseQuery reads a GET /api/v1/search request's parameters into a Query,
// refusing an invalid one with ErrInvalidQuery.
func ParseQuery(values url.Values) (Query, error) {
	var query Query
	for _, filter := range filters {
		if err := filter.parse(values, &query); err != nil {
			return Query{}, err
		}
	}
	return query, nil
}

// Parameters are the query parameters ParseQuery reads, in table order.
func Parameters() []string {
	var params []string
	for _, filter := range filters {
		params = append(params, filter.params...)
	}
	return params
}

// buildFilter translates a parsed Query into a Typesense filter_by
// expression: every filter's clauses, combined with AND.
func buildFilter(query Query) string {
	var clauses []string
	for _, filter := range filters {
		clauses = append(clauses, filter.clauses(query)...)
	}
	return strings.Join(clauses, " && ")
}

// parseCreatedBound reads a created_after (end false) or created_before (end
// true) bound: a date, which covers its whole UTC day, or an RFC 3339
// timestamp. Empty is no bound.
func parseCreatedBound(param, raw string, end bool) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	var at time.Time
	if date, err := time.Parse(time.DateOnly, raw); err == nil {
		at = date
		if end {
			at = date.AddDate(0, 0, 1).Add(-time.Second)
		}
	} else if stamp, err := time.Parse(time.RFC3339, raw); err == nil {
		at = stamp
	} else {
		return nil, ErrInvalidQuery.Withf("%s must be a date (2026-11-14) or an RFC 3339 timestamp", param)
	}
	unix := at.Unix()
	return &unix, nil
}

// listFilter is a comma-separated list parameter OR-matched against a facet
// field: any document carrying one of the values matches.
func listFilter(param, field string, target func(*Query) *[]string) filter {
	return filter{
		params: []string{param},
		parse: func(values url.Values, query *Query) error {
			*target(query) = splitList(values.Get(param))
			return nil
		},
		fields: []string{field},
		clauses: func(query Query) []string {
			list := *target(&query)
			if len(list) == 0 {
				return nil
			}
			return []string{field + ":=" + filterValueList(list)}
		},
	}
}

// qualityClause OR-matches names, or with a quality their quality facts.
func qualityClause(names []string, quality, nameField, factField string) []string {
	if len(names) == 0 {
		return nil
	}
	if quality == "" {
		return []string{nameField + ":=" + filterValueList(names)}
	}
	facts := make([]string, len(names))
	for i, name := range names {
		facts[i] = qualityFact(quality, name)
	}
	return []string{factField + ":=" + filterValueList(facts)}
}

// parseFilterList reads a JSON array parameter of at most maxNumericFilters
// filters into target.
func parseFilterList(values url.Values, param string, target any) error {
	raw := strings.TrimSpace(values.Get(param))
	if raw == "" {
		return nil
	}
	if raw[0] != '[' || json.Unmarshal([]byte(raw), target) != nil {
		return ErrInvalidQuery.Withf("%s must be a JSON array", param)
	}
	var count []json.RawMessage
	_ = json.Unmarshal([]byte(raw), &count)
	if len(count) > maxNumericFilters {
		return ErrInvalidQuery.Withf("%s contains too many filters (at most %d)", param, maxNumericFilters)
	}
	return nil
}

// validComparison is one of >, = or < against a finite, non-negative value.
func validComparison(operator string, value *float64) bool {
	return (operator == ">" || operator == "=" || operator == "<") &&
		value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0
}

func validMaterial(kind string) bool { return kind == "item" || kind == "fluid" }

// compareClause compares a numeric field; operator is one validComparison
// accepts (Typesense writes them field:>5, field:=5, field:<5).
func compareClause(field, operator string, value float64) string {
	return field + ":" + operator + formatFloat(value)
}

// splitList parses a comma-separated parameter into trimmed, non-empty values.
func splitList(raw string) []string {
	var values []string
	for item := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

// boundedInt reads an integer, falling back when it is missing or out of
// range.
func boundedInt(raw string, fallback, low, high int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < low || value > high {
		return fallback
	}
	return value
}

// buildFootprintFilter keeps entries that fit in width x height, either way
// round: the short side within the smaller bound, the long side within the
// larger. One bound alone limits the short side.
func buildFootprintFilter(width, height int) []string {
	switch {
	case width > 0 && height > 0:
		return []string{
			"footprint_short:<=" + strconv.Itoa(min(width, height)),
			"footprint_long:<=" + strconv.Itoa(max(width, height)),
		}
	case width > 0 || height > 0:
		return []string{"footprint_short:<=" + strconv.Itoa(max(width, height))}
	}
	return nil
}

// buildGameVersionFilter selects one exact version, or a release family
// (versionGroups) for the four selectors the browse page offers.
func buildGameVersionFilter(version string) string {
	switch version {
	case versionPre20, versionV20, versionV21, versionV21Compatible:
		return "version_groups:=" + filterValue(version)
	default:
		return "game_version:=" + filterValue(version)
	}
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func filterValue(v string) string {
	return "`" + strings.ReplaceAll(v, "`", "") + "`"
}

func filterValueList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = filterValue(v)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
