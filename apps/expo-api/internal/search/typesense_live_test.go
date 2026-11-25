package search

import (
	"context"
	"net/url"
	"os"
	"reflect"
	"slices"
	"testing"

	"factorio-expo/internal/shortid"
)

// liveClient is a client for a throwaway collection version on the Typesense
// at TEST_TYPESENSE_URL (with TEST_TYPESENSE_API_KEY), e.g.
// `docker run -p 8108:8108 typesense/typesense:30.2 --data-dir /tmp --api-key=x`.
// The test is skipped without it, like the PostgreSQL ones without
// TEST_DATABASE_URL.
func liveClient(t *testing.T) *TypesenseClient {
	t.Helper()
	baseURL := os.Getenv("TEST_TYPESENSE_URL")
	if baseURL == "" {
		t.Skip("TEST_TYPESENSE_URL is not set")
	}
	client := NewTypesenseClient(baseURL, os.Getenv("TEST_TYPESENSE_API_KEY"), nil)
	client.names = namesForVersion("live" + shortid.New())
	client.queried.Store(&client.names)
	ctx := context.Background()
	if err := client.createCollections(ctx); err != nil {
		t.Fatalf("create collections: %v", err)
	}
	t.Cleanup(func() { _ = client.dropCollection(ctx, client.names.Blueprints) })
	return client
}

// liveDocument is a blueprint document with every list field set, as the
// projection writes them.
func liveDocument(id, text string) blueprintDocument {
	return blueprintDocument{
		ID: id, EntryID: "entry" + id[:7], Kind: "blueprint", NodeID: id, Title: text, SearchText: text,
		Icons: []string{}, Tags: []string{}, Mods: []string{"vanilla"}, ModBadges: []string{},
		GameVersion: "2.1.3", VersionGroups: []string{"v2.1", "v2.1-compatible"},
		EntityNames: []string{}, Recipes: []string{}, EntityFacts: []string{}, RecipeFacts: []string{},
		Qualities: []string{}, ProductItems: []string{}, IngredientItems: []string{},
		LeafCount: 1, CreatedAt: 1000, UpdatedAt: 1000, rates: map[string]float64{},
	}
}

func (d blueprintDocument) producing(rate float64) blueprintDocument {
	d.TopProduct = "iron-gear-wheel"
	d.rates = map[string]float64{"rate_out_iron_gear_wheel": rate}
	return d
}

func nodeIDs(result Result) []string {
	ids := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		ids = append(ids, hit.NodeID)
	}
	return ids
}

func TestLiveTypesenseSearch(t *testing.T) {
	client := liveClient(t)
	ctx := context.Background()

	mall := liveDocument("mallmallmall", "Automation science pack mall").producing(4)
	mall.Tags, mall.CreatedAt = []string{"smelting"}, 1_000_000
	redBase := liveDocument("redredredred", "my red science base").producing(5)
	redBase.CreatedAt = 2_000_000
	circuits := liveDocument("greengreengr", "green circuits").producing(4.5)
	circuits.Tags, circuits.Mods, circuits.CreatedAt = []string{"oil"}, []string{"space-age"}, 3_000_000
	ship := liveDocument("shipshipship", "ship").producing(4.2)
	ship.HasPlatform, ship.CreatedAt = true, 4_000_000
	// A leaf inside a book called "science": its own text is "gears", and the
	// book's text is no longer matched through its leaves.
	gears := liveDocument("gearsgearsge", "gears")
	gears.BookTitle = "science book"
	book := liveDocument("book:bookbook", "science book")
	book.Kind, book.NodeID, book.Position = "book", "", -1

	var docs []map[string]any
	for _, doc := range []blueprintDocument{mall, redBase, circuits, ship, gears, book} {
		m, err := doc.toMap()
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, m)
	}
	if err := client.importDocuments(ctx, client.names.Blueprints, docs); err != nil {
		t.Fatal(err)
	}

	search := func(query Query) Result {
		t.Helper()
		result, err := client.Search(ctx, query)
		if err != nil {
			t.Fatalf("search %+v: %v", query, err)
		}
		return result
	}
	sorted := func(ids []string) []string { slices.Sort(ids); return ids }

	t.Run("community names find the internal name's results", func(t *testing.T) {
		byName := sorted(nodeIDs(search(Query{Text: "red science"})))
		byItem := sorted(nodeIDs(search(Query{Text: "automation-science-pack"})))
		byWords := sorted(nodeIDs(search(Query{Text: "automation science pack"})))
		want := []string{"mallmallmall", "redredredred"}
		if !reflect.DeepEqual(byName, want) || !reflect.DeepEqual(byItem, want) || !reflect.DeepEqual(byWords, want) {
			t.Fatalf("red science = %v, automation-science-pack = %v, words = %v, want %v", byName, byItem, byWords, want)
		}
	})

	t.Run("a book's text does not make its leaves match", func(t *testing.T) {
		got := sorted(nodeIDs(search(Query{Text: "science", Kinds: []string{"blueprint", "book"}})))
		if want := []string{"", "mallmallmall", "redredredred"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("science = %v, want %v (the book and the leaves with the word, not gears)", got, want)
		}
	})

	t.Run("date range", func(t *testing.T) {
		values := url.Values{"created_after": {"1970-01-24"}, "created_before": {"1970-02-08"}}
		query, err := ParseQuery(values)
		if err != nil {
			t.Fatal(err)
		}
		// 1970-01-24 is second 1_987_200 and 1970-02-08 ends at second 3_369_599.
		if got, want := sorted(nodeIDs(search(query))), []string{"greengreengr", "redredredred"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("range = %v, want %v", got, want)
		}
	})

	t.Run("facet counts leave their own filter out", func(t *testing.T) {
		result := search(Query{Facets: true, Kinds: []string{"blueprint"}, Tags: []string{"smelting"}})
		if result.Total != 1 {
			t.Fatalf("total = %d, want the one smelting blueprint", result.Total)
		}
		facets := result.Facets
		// Tags ignore the tag filter; the rest count only the smelting blueprint.
		if facets.Tags["smelting"] != 1 || facets.Tags["oil"] != 1 {
			t.Errorf("tags = %v, want smelting 1 and oil 1", facets.Tags)
		}
		if want := map[string]int64{"factories": 1}; !reflect.DeepEqual(facets.Platform, want) {
			t.Errorf("platform = %v, want %v", facets.Platform, want)
		}
		if want := map[string]int64{"vanilla": 1}; !reflect.DeepEqual(facets.Mods, want) {
			t.Errorf("mods = %v, want %v", facets.Mods, want)
		}
		if want := map[string]int64{"v2.1": 1, "v2.1-compatible": 1}; !reflect.DeepEqual(facets.Versions, want) {
			t.Errorf("versions = %v, want %v", facets.Versions, want)
		}
		// Kind is counted without the kind filter: the smelting book is none, nothing else.
		if want := map[string]int64{"blueprint": 1}; !reflect.DeepEqual(result.KindCounts, want) {
			t.Errorf("kind counts = %v, want %v", result.KindCounts, want)
		}
		all := search(Query{Facets: true, Kinds: []string{"blueprint"}, PlatformsOnly: true})
		if want := map[string]int64{"factories": 4, "platforms": 1}; !reflect.DeepEqual(all.Facets.Platform, want) {
			t.Errorf("platform counts with the type filter on = %v, want %v", all.Facets.Platform, want)
		}
	})

	t.Run("similar blueprints", func(t *testing.T) {
		result := search(Query{SimilarTo: "mallmallmall"})
		// The seed makes 4 gears/s: green circuits (4.5) and the red base (5) are
		// ground blueprints of the same top product; the ship is a platform.
		if got, want := nodeIDs(result), []string{"greengreengr", "redredredred"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("similar = %v, want %v", got, want)
		}
		if result.Total != 2 {
			t.Fatalf("total = %d, want 2", result.Total)
		}
		ship := search(Query{SimilarTo: "shipshipship"})
		if len(ship.Hits) != 0 {
			t.Fatalf("a platform is similar to %v, want none", nodeIDs(ship))
		}
		tagged := search(Query{SimilarTo: "mallmallmall", Tags: []string{"oil"}})
		if got, want := nodeIDs(tagged), []string{"greengreengr"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("similar with a tag = %v, want %v", got, want)
		}
		if _, err := client.Search(ctx, Query{SimilarTo: "nopenopenope"}); err == nil {
			t.Fatal("similar_to an unknown id succeeded")
		}
		if _, err := client.Search(ctx, Query{SimilarTo: "book:bookbook"}); err == nil {
			t.Fatal("similar_to a book succeeded")
		}
	})
}
