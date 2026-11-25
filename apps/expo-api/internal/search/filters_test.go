package search

import (
	"errors"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestParseQuery(t *testing.T) {
	values := url.Values{
		"q":                  {"[fluid=water] cooling tower"},
		"tags":               {"logistics, smelting,"},
		"entities":           {"biolab"},
		"entity_quality":     {"legendary"},
		"kind":               {"blueprint,book"},
		"footprint_width":    {"40"},
		"footprint_height":   {"nope"},
		"blueprint_type":     {"all"},
		"platform_filters":   {`[{"metric":"weight_tons","operator":">","value":100}]`},
		"production_filters": {`[{"type":"item","product":" iron-plate ","quality":"rare","direction":"out_per_tile","operator":">","rate":0.5}]`},
		"cost_filters":       {`[{"type":"fluid","resource":"crude-oil","operator":"=","amount":0}]`},
		"spoiler":            {"exclude"},
		"sort":               {"view_count:desc"},
		"limit":              {"500"},
		"offset":             {"40"},
	}
	got, err := ParseQuery(values)
	if err != nil {
		t.Fatal(err)
	}
	want := Query{
		Text: "cooling tower", Icons: []string{"[fluid=water]"},
		Tags: []string{"logistics", "smelting"}, Entities: []string{"biolab"}, EntityQuality: "legendary",
		Kinds: []string{"blueprint", "book"}, FootprintWidth: 40,
		PlatformFilters: []PlatformFilter{{Metric: "weight_tons", Operator: ">", Value: 100}},
		ProductionFilters: []ProductionFilter{
			{Item: "iron-plate", Quality: "rare", Direction: DirectionOutPerTile, Operator: ">", Rate: 0.5},
		},
		CostFilters: []CostFilter{{Resource: "crude-oil", Operator: "=", Amount: 0}},
		Spoiler:     SpoilerExclude, Sort: "view_count:desc", Limit: DefaultLimit, Offset: 40,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseQuery =\n%+v\nwant\n%+v", got, want)
	}
}

// The blueprint type drops the numeric filters it rules out, after checking them.
func TestParseQueryBlueprintType(t *testing.T) {
	values := url.Values{
		"production_filters": {`[{"type":"item","product":"iron-plate","direction":"out","operator":">","rate":5}]`},
		"platform_filters":   {`[{"metric":"weight_tons","operator":">","value":100}]`},
	}
	for blueprintType, want := range map[string][2]bool{
		"all": {true, true}, "factories": {true, false}, "platforms": {false, true},
	} {
		values.Set("blueprint_type", blueprintType)
		query, err := ParseQuery(values)
		if err != nil {
			t.Fatal(err)
		}
		if got := [2]bool{len(query.ProductionFilters) > 0, len(query.PlatformFilters) > 0}; got != want {
			t.Errorf("%s: production, platform = %v, want %v", blueprintType, got, want)
		}
	}
	values.Set("production_filters", `[{"type":"item","product":"iron-plate","direction":"out","operator":"!=","rate":5}]`)
	if _, err := ParseQuery(values); err == nil {
		t.Error("platforms: an invalid production filter passed")
	}
}

func TestParseQueryRejects(t *testing.T) {
	repeat := func(filter string) string {
		return "[" + strings.Repeat(filter+",", maxNumericFilters) + filter + "]"
	}
	for _, values := range []url.Values{
		{"blueprint_type": {"unknown"}},
		{"sort": {"title:asc"}},
		{"entity_quality": {"master"}},
		{"recipe_quality": {"master"}},
		{"kind": {"blueprint,map"}},
		{"spoiler": {"maybe"}},
		{"created_after": {"yesterday"}},
		{"created_before": {"2026-13-40"}},
		{"created_after": {"2026-11-14T10:00:00"}},
		{"facets": {"yes"}},
		{"similar_to": {"not an id"}},
		{"production_filters": {`[{"type":"item","product":"iron-plate","direction":"sideways","operator":">","rate":5}]`}},
		{"production_filters": {`[{"type":"item","product":"iron-plate","direction":"out","operator":"!=","rate":5}]`}},
		{"production_filters": {`[{"type":"item","product":"iron-plate","direction":"out","operator":">","rate":-1}]`}},
		{"production_filters": {`[{"type":"item","product":"iron-plate","direction":"out","operator":">"}]`}},
		{"production_filters": {`[{"type":"item","product":"iron-plate","quality":"master","direction":"out","operator":">","rate":5}]`}},
		// The material type is required, and fluids have no quality.
		{"production_filters": {`[{"product":"iron-plate","direction":"out","operator":">","rate":5}]`}},
		{"production_filters": {`[{"type":"signal","product":"iron-plate","direction":"out","operator":">","rate":5}]`}},
		{"production_filters": {`[{"type":"fluid","product":"water","quality":"rare","direction":"out","operator":">","rate":5}]`}},
		{"production_filters": {`{}`}},
		{"production_filters": {repeat(`{"type":"item","product":"iron-plate","direction":"out","operator":">","rate":5}`)}},
		{"platform_filters": {`[{"metric":"other","operator":">","value":5}]`}},
		{"platform_filters": {`[{"metric":"thrust_mn","operator":"!=","value":5}]`}},
		{"platform_filters": {`[{"metric":"width_tiles","operator":"<","value":10}]`}},
		{"platform_filters": {`[{"metric":"weight_tons","operator":"<","value":-1}]`}},
		{"platform_filters": {`[{"metric":"weight_tons","operator":"<","value":null}]`}},
		{"platform_filters": {`null`}},
		{"platform_filters": {`{}`}},
		{"platform_filters": {repeat(`{"metric":"weight_tons","operator":"<","value":1}`)}},
		{"cost_filters": {`[{"type":"item","resource":"iron-ore","operator":"!=","amount":5}]`}},
		{"cost_filters": {`[{"type":"item","resource":"iron-ore","operator":"<","amount":-1}]`}},
		{"cost_filters": {`[{"type":"item","resource":"iron-ore","operator":"<"}]`}},
		{"cost_filters": {`[{"type":"signal","resource":"iron-ore","operator":"<","amount":5}]`}},
		{"cost_filters": {`[{"type":"item","resource":" ","operator":"<","amount":5}]`}},
		{"cost_filters": {`{}`}},
		{"cost_filters": {repeat(`{"type":"item","resource":"iron-ore","operator":"<","amount":5}`)}},
	} {
		if _, err := ParseQuery(values); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("ParseQuery(%v) = %v, want ErrInvalidQuery", values, err)
		}
	}
}

// Every field a filter compares is declared in the blueprints schema and
// written by the projection, and every clause compares only its filter's
// fields: a field renamed on one side fails here, not in Typesense.
func TestFilterFieldsMatchSchemaAndProjection(t *testing.T) {
	declared := map[string]bool{}
	for _, field := range schemaBlueprints.Fields {
		declared[field.Name] = true
	}
	directions := []string{DirectionOut, DirectionIn, DirectionOutPerTile, DirectionOutPerMW}
	written := map[string]bool{}
	documentType := reflect.TypeFor[blueprintDocument]()
	for field := range documentType.Fields() {
		if tag := field.Tag.Get("json"); tag != "" {
			written[strings.Split(tag, ",")[0]] = true
		}
	}
	// The dynamic fields buildRateFields writes.
	for _, direction := range directions {
		written[rateFieldName(direction, "rare", "iron-plate")] = true
	}
	written[costFieldName("iron-ore")] = true

	full := Query{
		Icons: []string{"[item=iron-plate]"}, Tags: []string{"t"}, Mods: []string{"m"},
		Entities: []string{"e"}, Recipes: []string{"r"}, GameVersion: "v2.1-compatible",
		Kinds: []string{"blueprint"}, FootprintWidth: 2, FootprintHeight: 3,
		PlatformsOnly: true, Spoiler: SpoilerOnly, CreatedAfter: new(int64(1)), CreatedBefore: new(int64(2)),
		CostFilters: []CostFilter{{Resource: "iron-ore", Operator: "=", Amount: 1}},
	}
	for _, metric := range platformMetrics {
		full.PlatformFilters = append(full.PlatformFilters, PlatformFilter{Metric: metric, Operator: ">", Value: 1})
	}
	for _, direction := range directions {
		full.ProductionFilters = append(full.ProductionFilters,
			ProductionFilter{Item: "iron-plate", Quality: "rare", Direction: direction, Operator: "<", Rate: 1})
	}
	qualities := full
	qualities.EntityQuality, qualities.RecipeQuality = "rare", "rare"
	// A clause's fields: at its start, or inside a parenthesised OR.
	clauseField := regexp.MustCompile(`(?:^|\(|\|\| )([a-z0-9_]+):`)

	for _, filter := range filters {
		patterns := make([]*regexp.Regexp, 0, len(filter.fields))
		for _, field := range filter.fields {
			if !declared[field] {
				t.Errorf("%v: field %q is not in schemaBlueprints", filter.params, field)
			}
			pattern := regexp.MustCompile("^" + field + "$")
			patterns = append(patterns, pattern)
			found := false
			for name := range written {
				found = found || pattern.MatchString(name)
			}
			if !found {
				t.Errorf("%v: field %q is not written by the projection", filter.params, field)
			}
		}
		for _, query := range []Query{full, qualities} {
			clauses := filter.clauses(query)
			if len(filter.fields) > 0 && len(clauses) == 0 {
				t.Errorf("%v: no clause for a full query", filter.params)
			}
			for _, clause := range clauses {
				for _, match := range clauseField.FindAllStringSubmatch(clause, -1) {
					ok := false
					for _, pattern := range patterns {
						ok = ok || pattern.MatchString(match[1])
					}
					if !ok {
						t.Errorf("%v: clause %q compares %q, not one of %v", filter.params, clause, match[1], filter.fields)
					}
				}
			}
		}
	}
}

// A bare date covers its whole UTC day on either bound; a timestamp is exact.
func TestParseQueryCreatedRange(t *testing.T) {
	query, err := ParseQuery(url.Values{"created_after": {"2026-11-14"}, "created_before": {"2026-11-16"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := buildFilter(query), "created_at:>=1794614400 && created_at:<=1794873599"; got != want {
		t.Errorf("date range filter = %q, want %q", got, want)
	}
	query, err = ParseQuery(url.Values{
		"created_after": {"2026-11-14T10:00:00+02:00"}, "created_before": {"2026-11-14T10:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := buildFilter(query), "created_at:>=1794643200 && created_at:<=1794650400"; got != want {
		t.Errorf("timestamp range filter = %q, want %q", got, want)
	}
	if query, _ := ParseQuery(url.Values{}); buildFilter(query) != "" {
		t.Errorf("no range filtered %q", buildFilter(query))
	}
}

func TestParseQueryFacetsAndSimilar(t *testing.T) {
	query, err := ParseQuery(url.Values{"facets": {"true"}, "similar_to": {"abcdefghijkl"}})
	if err != nil {
		t.Fatal(err)
	}
	if !query.Facets || query.SimilarTo != "abcdefghijkl" {
		t.Errorf("query = %+v", query)
	}
}
