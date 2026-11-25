package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Each facet is counted with its own filter left out: the tags filter is set,
// so tags are counted by a second request without it, while the main request
// facets on every facet whose filter is not set.
func TestSearchFacetCountsLeaveOwnFilterOut(t *testing.T) {
	var mu sync.Mutex
	type request struct{ filter, facetBy, perPage string }
	var requests []request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		mu.Lock()
		requests = append(requests, request{query.Get("filter_by"), query.Get("facet_by"), query.Get("per_page")})
		mu.Unlock()
		if query.Get("facet_by") == "tags" {
			fmt.Fprint(w, `{"found":9,"hits":[],"facet_counts":[{"field_name":"tags","counts":[{"value":"smelting","count":6},{"value":"oil","count":3}]}]}`)
			return
		}
		fmt.Fprint(w, `{"found":6,"hits":[],"facet_counts":[
			{"field_name":"kind","counts":[{"value":"blueprint","count":6}]},
			{"field_name":"mods","counts":[{"value":"vanilla","count":5},{"value":"space-age","count":1}]},
			{"field_name":"version_groups","counts":[{"value":"v2.1","count":6},{"value":"v2.1-compatible","count":6}]},
			{"field_name":"has_platform","counts":[{"value":"false","count":5},{"value":"true","count":1}]}]}`)
	}))
	defer server.Close()
	client := NewTypesenseClient(server.URL, "test", server.Client())

	result, err := client.Search(context.Background(), Query{Facets: true, Tags: []string{"smelting"}, Spoiler: SpoilerExclude})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].facetBy < requests[j].facetBy })
	want := []request{
		{"tags:=[`smelting`] && spoiler:=false", "kind,mods,version_groups,has_platform", ""},
		{"spoiler:=false", "tags", "0"},
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %+v, want %+v", requests, want)
	}
	wantFacets := &Facets{
		Tags:     map[string]int64{"smelting": 6, "oil": 3},
		Platform: map[string]int64{"factories": 5, "platforms": 1},
		Mods:     map[string]int64{"vanilla": 5, "space-age": 1},
		Versions: map[string]int64{"v2.1": 6, "v2.1-compatible": 6},
	}
	if !reflect.DeepEqual(result.Facets, wantFacets) {
		t.Fatalf("facets = %+v, want %+v", result.Facets, wantFacets)
	}
}

// Without Facets only kind is counted, and the result carries no facet counts.
func TestSearchFacetsAreOptIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("facet_by"); got != "kind" {
			t.Errorf("facet_by = %q, want kind", got)
		}
		fmt.Fprint(w, `{"found":0,"hits":[]}`)
	}))
	defer server.Close()
	client := NewTypesenseClient(server.URL, "test", server.Client())
	result, err := client.Search(context.Background(), Query{})
	if err != nil || result.Facets != nil {
		t.Fatalf("facets = %+v, err = %v", result.Facets, err)
	}
}

// Clearing one facet's filter leaves every other filter of the query alone.
func TestFacetSpecsClearOnlyTheirFilter(t *testing.T) {
	query := Query{
		Tags: []string{"a"}, Mods: []string{"vanilla"}, GameVersion: "v2.1", Kinds: []string{"book"},
		PlatformsOnly: true, Spoiler: SpoilerExclude,
	}
	for _, spec := range facetSpecs {
		cleared := query
		spec.clear(&cleared)
		if spec.active(cleared) {
			t.Errorf("%s still active after clear", spec.field)
		}
		for _, other := range facetSpecs {
			if other.field != spec.field && !other.active(cleared) {
				t.Errorf("clearing %s also cleared %s", spec.field, other.field)
			}
		}
		if cleared.Spoiler != SpoilerExclude {
			t.Errorf("clearing %s dropped the spoiler filter", spec.field)
		}
	}
	if inline, separate := splitFacets(Query{Facets: true, Tags: []string{"a"}}); len(separate) != 1 ||
		strings.Contains(strings.Join(inline, ","), "tags") {
		t.Errorf("inline = %v, separate = %v", inline, separate)
	}
}
