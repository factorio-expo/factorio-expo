package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/search"
)

type queryCaptureClient struct {
	search.Client
	query search.Query
}

func (c *queryCaptureClient) Search(_ context.Context, query search.Query) (search.Result, error) {
	c.query = query
	return search.Result{}, nil
}

func TestSearchEntriesBlueprintType(t *testing.T) {
	for _, test := range []struct {
		name       string
		typeValue  string
		platforms  bool
		factories  bool
		production bool
		metrics    bool
	}{
		{name: "all", typeValue: "all", production: true, metrics: true},
		{name: "factories", typeValue: "factories", factories: true, production: true},
		{name: "platforms", typeValue: "platforms", platforms: true, metrics: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &queryCaptureClient{}
			s := &server{search: client}
			params := url.Values{
				"blueprint_type":     {test.typeValue},
				"production_filters": {`[{"type":"item","product":"iron-plate","direction":"out","operator":">","rate":5}]`},
				"platform_filters":   {`[{"metric":"weight_tons","operator":">","value":100}]`},
			}
			response := httptest.NewRecorder()
			serveRoutes(s, response, httptest.NewRequest("GET", "/api/v1/search?"+params.Encode(), nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if client.query.PlatformsOnly != test.platforms || client.query.FactoriesOnly != test.factories ||
				(len(client.query.ProductionFilters) > 0) != test.production ||
				(len(client.query.PlatformFilters) > 0) != test.metrics {
				t.Fatalf("unexpected query: %+v", client.query)
			}
		})
	}
}

func TestSearchEntriesRejectsInvalidBlueprintType(t *testing.T) {
	s := &server{}
	response := httptest.NewRecorder()
	serveRoutes(s, response, httptest.NewRequest("GET", "/api/v1/search?blueprint_type=unknown", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
}

func TestSearchEntriesRejectsInvalidSort(t *testing.T) {
	s := &server{}
	request := httptest.NewRequest("GET", "/api/v1/search?sort=title:asc", nil)
	response := httptest.NewRecorder()
	serveRoutes(s, response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", response.Code)
	}
}

func TestSearchEntriesRejectsUnknownQualityFilters(t *testing.T) {
	s := &server{}
	request := httptest.NewRequest("GET", "/api/v1/search?entity_quality=master", nil)
	response := httptest.NewRecorder()
	serveRoutes(s, response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", response.Code)
	}
}

func TestSearchEntriesSpoilerFilter(t *testing.T) {
	for _, value := range []string{"", "exclude", "only"} {
		client := &queryCaptureClient{}
		s := &server{search: client}
		response := httptest.NewRecorder()
		serveRoutes(s, response, httptest.NewRequest("GET", "/api/v1/search?spoiler="+value, nil))
		if response.Code != http.StatusOK || client.query.Spoiler != value {
			t.Fatalf("spoiler=%q: status %d, query %+v", value, response.Code, client.query)
		}
	}
	response := httptest.NewRecorder()
	serveRoutes(&server{}, response, httptest.NewRequest("GET", "/api/v1/search?spoiler=maybe", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
}

// serveRoutes serves request through the public route table, as New does
// without its outer middleware.
func serveRoutes(s *server, response http.ResponseWriter, request *http.Request) {
	s.routes().ServeHTTP(response, request)
}

type resultClient struct {
	search.Client
	result search.Result
	err    error
	query  search.Query
}

func (c *resultClient) Search(_ context.Context, query search.Query) (search.Result, error) {
	c.query = query
	return c.result, c.err
}

func searchRequest(s *server, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	serveRoutes(s, response, httptest.NewRequest("GET", target, nil))
	return response
}

func TestSearchEntriesFacetCounts(t *testing.T) {
	client := &resultClient{result: search.Result{
		KindCounts: map[string]int64{"blueprint": 3},
		Facets: &search.Facets{
			Tags: map[string]int64{"oil": 2}, Platform: map[string]int64{"platforms": 1, "factories": 2},
			Mods: map[string]int64{"vanilla": 3}, Versions: map[string]int64{"v2.1": 3},
		},
	}}
	response := searchRequest(&server{search: client}, "/api/v1/search?facets=true")
	if response.Code != http.StatusOK || !client.query.Facets {
		t.Fatalf("status = %d, facets requested = %v, body = %s", response.Code, client.query.Facets, response.Body.String())
	}
	var body struct {
		FacetCounts struct {
			Tags     map[string]int64 `json:"tags"`
			Platform map[string]int64 `json:"platform"`
			Mods     map[string]int64 `json:"mods"`
			Versions map[string]int64 `json:"versions"`
		} `json:"facet_counts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.FacetCounts.Tags["oil"] != 2 || body.FacetCounts.Platform["platforms"] != 1 ||
		body.FacetCounts.Mods["vanilla"] != 3 || body.FacetCounts.Versions["v2.1"] != 3 {
		t.Fatalf("facet_counts = %+v", body.FacetCounts)
	}

	// Not asked for: no facet_counts key.
	plain := searchRequest(&server{search: &resultClient{}}, "/api/v1/search")
	if strings.Contains(plain.Body.String(), "facet_counts") {
		t.Fatalf("body has facet_counts without facets=true: %s", plain.Body.String())
	}
}

func TestSearchEntriesCreatedRange(t *testing.T) {
	client := &resultClient{}
	response := searchRequest(&server{search: client}, "/api/v1/search?created_after=2026-11-01&created_before=2026-11-30T12:00:00Z")
	if response.Code != http.StatusOK || client.query.CreatedAfter == nil || client.query.CreatedBefore == nil {
		t.Fatalf("status = %d, query = %+v", response.Code, client.query)
	}
	for _, bad := range []string{"created_after=soon", "created_before=2026-02-31"} {
		if response := searchRequest(&server{search: client}, "/api/v1/search?"+bad); response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, response.Code)
		}
	}
}

func TestSearchEntriesSimilarTo(t *testing.T) {
	client := &resultClient{}
	if response := searchRequest(&server{search: client}, "/api/v1/search?similar_to=abcdefghijkl"); response.Code != http.StatusOK ||
		client.query.SimilarTo != "abcdefghijkl" {
		t.Fatalf("status = %d, query = %+v", response.Code, client.query)
	}
	gone := &resultClient{err: search.ErrSimilarNotFound}
	if response := searchRequest(&server{search: gone}, "/api/v1/search?similar_to=abcdefghijkl"); response.Code != http.StatusNotFound {
		t.Fatalf("unknown seed: status = %d, want 404", response.Code)
	}
	if response := searchRequest(&server{search: client}, "/api/v1/search?similar_to=not-an-id!"); response.Code != http.StatusBadRequest {
		t.Fatalf("malformed id: status = %d, want 400", response.Code)
	}
}
