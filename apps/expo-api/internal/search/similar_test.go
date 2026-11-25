package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSimilar answers the seed document and the two rate-ordered requests.
func fakeSimilar(t *testing.T, seed string, above, below []string, filters *[]string) *TypesenseClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/documents/") && !strings.HasSuffix(r.URL.Path, "/search") {
			if strings.HasSuffix(r.URL.Path, "/missing") {
				http.Error(w, `{"message":"Could not find a document"}`, http.StatusNotFound)
				return
			}
			fmt.Fprint(w, seed)
			return
		}
		query := r.URL.Query()
		*filters = append(*filters, query.Get("filter_by")+" | "+query.Get("sort_by"))
		hits := above
		if strings.Contains(query.Get("filter_by"), "rate_out_iron_gear_wheel:<") {
			hits = below
		}
		fmt.Fprintf(w, `{"found":%d,"hits":[%s]}`, len(hits)+10, strings.Join(hits, ","))
	}))
	t.Cleanup(server.Close)
	return NewTypesenseClient(server.URL, "test", server.Client())
}

func similarHit(title string, rate float64) string {
	return fmt.Sprintf(`{"document":{"entry_id":"e","node_id":"n","kind":"blueprint","title":%q,"rate_out_iron_gear_wheel":%v}}`, title, rate)
}

func titles(result Result) string {
	var got []string
	for _, hit := range result.Hits {
		got = append(got, hit.Title)
	}
	return strings.Join(got, ",")
}

func TestSearchSimilarOrdersByRateRatio(t *testing.T) {
	var filters []string
	client := fakeSimilar(t,
		`{"id":"abcdefghijkl","kind":"blueprint","top_product":"iron-gear-wheel","has_platform":false,"rate_out_iron_gear_wheel":4}`,
		// At or above 4/s: 4.5 is 1.125x, 8 is 2x, 20 is 5x.
		[]string{similarHit("1.125x", 4.5), similarHit("2x", 8), similarHit("5x", 20)},
		// Below: 3 is 0.75x, 2 is 0.5x (as far as 2x), 0.4 is 0.1x.
		[]string{similarHit("0.75x", 3), similarHit("0.5x", 2), similarHit("0.1x", 0.4)},
		&filters)
	result, err := client.Search(context.Background(), Query{
		SimilarTo: "abcdefghijkl", Limit: 4, Sort: "view_count:desc", Kinds: []string{"book"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 1.125x (ln 0.12) before 0.75x (0.29), then the tie 2x/0.5x lists the upper side first.
	if got, want := titles(result), "1.125x,0.75x,2x,0.5x"; got != want {
		t.Errorf("hits = %s, want %s", got, want)
	}
	if result.Total != 26 || result.KindCounts["blueprint"] != 26 || result.Facets != nil {
		t.Errorf("total = %d, kind counts = %v, facets = %v", result.Total, result.KindCounts, result.Facets)
	}
	wantFilter := "kind:=[`blueprint`] && top_product:=`iron-gear-wheel` && has_platform:=false && id:!=`abcdefghijkl`"
	if len(filters) != 2 ||
		filters[0] != wantFilter+" && rate_out_iron_gear_wheel:>=4 | rate_out_iron_gear_wheel:asc,created_at:desc,position:asc" ||
		filters[1] != wantFilter+" && rate_out_iron_gear_wheel:<4 | rate_out_iron_gear_wheel:desc,created_at:desc,position:asc" {
		t.Errorf("requests = %q", filters)
	}

	page, err := client.Search(context.Background(), Query{SimilarTo: "abcdefghijkl", Limit: 2, Offset: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := titles(page), "5x,0.1x"; got != want {
		t.Errorf("second page = %s, want %s", got, want)
	}
}

func TestSearchSimilarWithoutFactsOrBlueprint(t *testing.T) {
	var filters []string
	client := fakeSimilar(t, `{"id":"a","kind":"blueprint","top_product":"","has_platform":false}`, nil, nil, &filters)
	result, err := client.Search(context.Background(), Query{SimilarTo: "a"})
	if err != nil || result.Total != 0 || len(result.Hits) != 0 || len(filters) != 0 {
		t.Errorf("unanalysed seed: %+v, %v, %q", result, err, filters)
	}
	if _, err := client.Search(context.Background(), Query{SimilarTo: "missing"}); !errors.Is(err, ErrSimilarNotFound) {
		t.Errorf("missing seed: %v, want ErrSimilarNotFound", err)
	}
	book := fakeSimilar(t, `{"id":"book:x","kind":"book"}`, nil, nil, &filters)
	if _, err := book.Search(context.Background(), Query{SimilarTo: "book:x"}); !errors.Is(err, ErrSimilarNotFound) {
		t.Errorf("book seed: %v, want ErrSimilarNotFound", err)
	}
}
