package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSearchMissingProductionField(t *testing.T) {
	for _, direction := range []string{"out", "in"} {
		for _, operator := range []string{">", "=", "<"} {
			for _, status := range []int{http.StatusBadRequest, http.StatusNotFound} {
				t.Run(fmt.Sprintf("%s/%s/%d", direction, operator, status), func(t *testing.T) {
					field := "rate_" + direction + "_ammoniacal_solution"
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						want := "tags:=[`smelting`] && rate_out_iron_plate:>5 && " + field + ":" + operator + "0"
						if got := r.URL.Query().Get("filter_by"); got != want {
							t.Errorf("filter_by = %q, want %q", got, want)
						}
						w.WriteHeader(status)
						json.NewEncoder(w).Encode(map[string]string{
							"message": "Could not find a filter field named `" + field + "` in the schema.",
						})
					}))
					defer server.Close()
					client := NewTypesenseClient(server.URL, "test", server.Client())
					result, err := client.Search(context.Background(), Query{
						Tags: []string{"smelting"},
						ProductionFilters: []ProductionFilter{
							{Item: "iron-plate", Direction: "out", Operator: ">", Rate: 5},
							{Item: "ammoniacal-solution", Direction: direction, Operator: operator, Rate: 0},
						},
					})
					if err != nil {
						t.Fatal(err)
					}
					if result.Total != 0 || result.Hits == nil || len(result.Hits) != 0 {
						t.Errorf("result = %+v, want zero total and empty hits", result)
					}
				})
			}
		}
	}
}

// A cost filter on a resource no document carries matches nothing.
func TestSearchMissingCostField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Could not find a filter field named `cost_raw_uranium_ore` in the schema.",
		})
	}))
	defer server.Close()
	client := NewTypesenseClient(server.URL, "test", server.Client())
	result, err := client.Search(context.Background(), Query{
		CostFilters: []CostFilter{{Resource: "uranium-ore", Operator: "<", Amount: 5}},
	})
	if err != nil || result.Total != 0 || len(result.Hits) != 0 {
		t.Fatalf("result = %+v, err = %v; want an empty result", result, err)
	}
}

func TestSearchPreservesOtherErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"static field", 400, "{\"message\":\"Could not find a filter field named `tags` in the schema.\"}"},
		{"unrequested rate", 400, "{\"message\":\"Could not find a filter field named `rate_out_water` in the schema.\"}"},
		{"other direction", 400, "{\"message\":\"Could not find a filter field named `rate_in_ammoniacal_solution` in the schema.\"}"},
		{"server failure", 500, "{\"message\":\"Could not find a filter field named `rate_out_ammoniacal_solution` in the schema.\"}"},
		{"invalid filter", 400, "{\"message\":\"Error with filter field `rate_out_ammoniacal_solution`: Not a float.\"}"},
		{"missing collection", 404, "{\"message\":\"Not found.\"}"},
		{"invalid response", 400, "invalid JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			client := NewTypesenseClient(server.URL, "test", server.Client())
			_, err := client.Search(context.Background(), Query{
				ProductionFilters: []ProductionFilter{
					{Item: "ammoniacal-solution", Direction: "out", Operator: ">", Rate: 0},
				},
			})
			if err == nil {
				t.Fatal("expected search error")
			}
		})
	}
}

func TestSearchSortingAndPagination(t *testing.T) {
	tests := []struct {
		sort string
		want string
	}{
		{"", "updated_at:desc,created_at:desc,position:asc"},
		{"relevance", "_text_match:desc,updated_at:desc,position:asc"},
		{"created_at:asc", "created_at:asc,position:asc"},
		{"created_at:desc", "created_at:desc,position:asc"},
		{"updated_at:asc", "updated_at:asc,created_at:desc,position:asc"},
		{"updated_at:desc", "updated_at:desc,created_at:desc,position:asc"},
		{"entity_count:asc", "entity_count:asc,created_at:desc,position:asc"},
		{"entity_count:desc", "entity_count:desc,created_at:desc,position:asc"},
		{"leaf_count:asc", "leaf_count:asc,created_at:desc,position:asc"},
		{"leaf_count:desc", "leaf_count:desc,created_at:desc,position:asc"},
	}
	for _, tt := range tests {
		t.Run(tt.sort, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasPrefix(r.URL.Path, "/collections/blueprints_") {
					t.Errorf("searched %s, want the blueprints collection", r.URL.Path)
				}
				if got := r.URL.Query().Get("query_by"); got != "search_text" {
					t.Errorf("query_by = %q", got)
				}
				if got := r.URL.Query().Get("sort_by"); got != tt.want {
					t.Errorf("sort_by = %q, want %q", got, tt.want)
				}
				if got := r.URL.Query().Get("offset"); got != "25" {
					t.Errorf("offset = %q, want 25", got)
				}
				if got := r.URL.Query().Get("limit"); got != "10" {
					t.Errorf("limit = %q, want 10", got)
				}
				if got := r.URL.Query().Get("filter_by"); got != "tags:=[`smelting`]" {
					t.Errorf("filter_by = %q", got)
				}
				fmt.Fprint(w, `{"found":22,"hits":[]}`)
			}))
			defer server.Close()
			client := NewTypesenseClient(server.URL, "test", server.Client())
			result, err := client.Search(context.Background(), Query{
				Sort: tt.sort, Limit: 10, Offset: 25, Tags: []string{"smelting"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Total != 22 {
				t.Errorf("total = %d, want 22", result.Total)
			}
		})
	}
}

func TestSortExpressionRejectsUnknownFields(t *testing.T) {
	for _, sort := range []string{"title:asc", "created_at", "created_at:desc,leaf_count:asc", "entity_count:invalid"} {
		if _, err := SortExpression(sort); err == nil {
			t.Errorf("SortExpression(%q) succeeded", sort)
		}
	}
}

// With a kind selected, the counts come from a second facet-only search
// without the kind filter, so every kind keeps its count.
func TestSearchCountsEveryKind(t *testing.T) {
	var filters []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		filters = append(filters, query.Get("filter_by"))
		if query.Get("per_page") == "0" && query.Get("facet_by") != "kind" {
			t.Errorf("facet_by = %q, want kind", query.Get("facet_by"))
		}
		if query.Get("per_page") == "0" {
			fmt.Fprint(w, `{"found":5,"hits":[],"facet_counts":[{"field_name":"kind","counts":[{"value":"blueprint","count":4},{"value":"book","count":1}]}]}`)
			return
		}
		fmt.Fprint(w, `{"found":1,"hits":[],"facet_counts":[{"field_name":"kind","counts":[{"value":"book","count":1}]}]}`)
	}))
	defer server.Close()
	client := NewTypesenseClient(server.URL, "test", server.Client())

	result, err := client.Search(context.Background(), Query{Kinds: []string{"book"}, Tags: []string{"smelting"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"tags:=[`smelting`] && kind:=[`book`]", "tags:=[`smelting`]"}; !reflect.DeepEqual(filters, want) {
		t.Fatalf("filters = %q, want %q", filters, want)
	}
	if want := map[string]int64{"blueprint": 4, "book": 1}; !reflect.DeepEqual(result.KindCounts, want) {
		t.Fatalf("kind counts = %v, want %v", result.KindCounts, want)
	}

	// Without a kind, the one search's counts are already unfiltered.
	filters = nil
	result, err = client.Search(context.Background(), Query{})
	if err != nil || len(filters) != 1 || result.KindCounts["book"] != 1 {
		t.Fatalf("filters = %q, counts = %v, err = %v", filters, result.KindCounts, err)
	}
}
