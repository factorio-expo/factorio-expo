package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeTypesense keeps just enough collection and alias state to drive the
// index lifecycle.
type fakeTypesense struct {
	mu          sync.Mutex
	collections map[string]collectionSchema
	documents   map[string]int64
	aliases     map[string]string
	requests    []string
	synonymSets []string
	failMethod  string
}

func newFakeTypesense(collections ...string) *fakeTypesense {
	f := &fakeTypesense{
		collections: map[string]collectionSchema{}, documents: map[string]int64{}, aliases: map[string]string{},
	}
	for _, name := range collections {
		f.collections[name] = collectionSchema{Name: name}
	}
	return f
}

func (f *fakeTypesense) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.Method == f.failMethod {
		http.Error(w, `{"message":"operation failed"}`, http.StatusConflict)
		return
	}
	name, isAlias := strings.CutPrefix(r.URL.Path, "/aliases/")
	switch {
	case isAlias && r.Method == http.MethodGet:
		target, ok := f.aliases[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"name": name, "collection_name": target})
	case isAlias && r.Method == http.MethodPut:
		var body struct {
			CollectionName string `json:"collection_name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.aliases[name] = body.CollectionName
	case strings.HasPrefix(r.URL.Path, "/synonym_sets/") && r.Method == http.MethodPut:
		f.synonymSets = append(f.synonymSets, strings.TrimPrefix(r.URL.Path, "/synonym_sets/"))
	case r.URL.Path == "/collections" && r.Method == http.MethodGet:
		var list []map[string]string
		for name := range f.collections {
			list = append(list, map[string]string{"name": name})
		}
		json.NewEncoder(w).Encode(list)
	case r.URL.Path == "/collections" && r.Method == http.MethodPost:
		var schema collectionSchema
		json.NewDecoder(r.Body).Decode(&schema)
		f.collections[schema.Name] = schema
		w.WriteHeader(http.StatusCreated)
	default:
		name := strings.TrimPrefix(r.URL.Path, "/collections/")
		if _, ok := f.collections[name]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodDelete:
			delete(f.collections, name)
			delete(f.documents, name)
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"name": name, "num_documents": f.documents[name]})
		}
	}
}

// live publishes the client's version as an empty, complete index.
func (f *fakeTypesense) live(names collectionNames) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.collections[names.Blueprints] = collectionSchema{Name: names.Blueprints}
	f.aliases["blueprints"] = names.Blueprints
}

func (f *fakeTypesense) collectionNames() []string {
	var names []string
	for name := range f.collections {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func newFakeClient(t *testing.T, fake *fakeTypesense) *TypesenseClient {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return NewTypesenseClient(server.URL, "test-key", server.Client())
}

func TestBuildAndPublishReplacesOtherVersions(t *testing.T) {
	fake := newFakeTypesense("blueprints", "blueprints_old", "unrelated")
	client := newFakeClient(t, fake)
	names := client.names

	if err := client.createCollections(context.Background()); err != nil {
		t.Fatalf("create collections: %v", err)
	}
	if err := client.publish(context.Background()); err != nil {
		t.Fatalf("publish: %v", err)
	}

	want := []string{names.Blueprints, "unrelated"}
	slices.Sort(want)
	if got := fake.collectionNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("collections = %v, want %v", got, want)
	}
	wantAliases := map[string]string{"blueprints": names.Blueprints}
	if !reflect.DeepEqual(fake.aliases, wantAliases) {
		t.Fatalf("aliases = %v, want %v", fake.aliases, wantAliases)
	}
	// Legacy collections (named like the aliases) go before the aliases are set, other versions only after the
	// blueprints alias (the completion marker) moved off them.
	var steps []string
	for _, request := range fake.requests {
		if path, ok := strings.CutPrefix(request, "DELETE /collections/"); ok && !strings.HasSuffix(path, names.Version) {
			steps = append(steps, "drop "+path)
		}
		if path, ok := strings.CutPrefix(request, "PUT /aliases/"); ok {
			steps = append(steps, "alias "+path)
		}
	}
	want = []string{
		"drop blueprints", "alias blueprints", "drop blueprints_old",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("publish steps = %v, want %v", steps, want)
	}
}

// A rebuild keeps searches on the published version until the new one is
// live, then switches them over and drops the old version.
func TestEnsureIndexSearchesPublishedVersionUntilBuilt(t *testing.T) {
	fake := newFakeTypesense("blueprints_old")
	fake.aliases["blueprints"] = "blueprints_old"
	client := newFakeClient(t, fake)

	var duringBuild string
	syncer := &Syncer{engine: client, queries: &listingQueries{list: func() {
		duringBuild = client.queried.Load().Blueprints
	}}}
	rebuilt, _, err := client.EnsureIndex(context.Background(), syncer)
	if err != nil || !rebuilt {
		t.Fatalf("ensure index = %v, %v; want a rebuild", rebuilt, err)
	}
	if duringBuild != "blueprints_old" {
		t.Fatalf("searches read %q while building, want blueprints_old", duringBuild)
	}
	if got := client.queried.Load().Blueprints; got != client.names.Blueprints {
		t.Fatalf("searches read %q after publishing, want %q", got, client.names.Blueprints)
	}
	if _, ok := fake.collections["blueprints_old"]; ok {
		t.Fatal("old version survived the publish")
	}
}

// listingQueries is a Syncer query set for an empty catalog; list runs when
// the rebuild lists the searchable entries.
type listingQueries struct {
	syncQueries
	list func()
}

func (q *listingQueries) ListSearchableEntryIDs(context.Context) ([]string, error) {
	q.list()
	return nil, nil
}

func TestEnsureIndexSkipsLiveVersion(t *testing.T) {
	fake := newFakeTypesense()
	client := newFakeClient(t, fake)
	fake.live(client.names)

	// A nil query set would panic if EnsureIndex tried to reindex.
	rebuilt, _, err := client.EnsureIndex(context.Background(), nil)
	if err != nil || rebuilt {
		t.Fatalf("ensure index = %v, %v; want no rebuild", rebuilt, err)
	}
	for _, request := range fake.requests {
		if !strings.HasPrefix(request, "GET ") {
			t.Fatalf("live index issued %s", request)
		}
	}
}

func TestIndexLiveDetectsOtherVersionsAndDanglingAliases(t *testing.T) {
	for name, setup := range map[string]func(*fakeTypesense, collectionNames){
		"no alias":       func(*fakeTypesense, collectionNames) {},
		"other version":  func(f *fakeTypesense, _ collectionNames) { f.aliases["blueprints"] = "blueprints_old" },
		"dangling alias": func(f *fakeTypesense, n collectionNames) { f.aliases["blueprints"] = n.Blueprints },
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeTypesense("blueprints_old")
			client := newFakeClient(t, fake)
			setup(fake, client.names)
			live, _, err := client.indexLive(context.Background())
			if err != nil || live {
				t.Fatalf("index live = %v, %v; want false", live, err)
			}
		})
	}
}

func TestCreateCollectionsStopsOnFailure(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			fake := newFakeTypesense()
			fake.failMethod = method
			client := newFakeClient(t, fake)
			err := client.createCollections(context.Background())
			if err == nil || !strings.Contains(err.Error(), "operation failed") {
				t.Fatalf("create collections error = %v, want Typesense error", err)
			}
			if last := fake.requests[len(fake.requests)-1]; !strings.HasPrefix(last, method+" ") {
				t.Fatalf("requests = %v, want none after the failing %s", fake.requests, method)
			}
		})
	}
}

func TestIndexVersionTracksSchema(t *testing.T) {
	before := indexVersion()
	original := logicalSchemas[0].Fields
	logicalSchemas[0].Fields = append(slices.Clone(original), schemaField{Name: "new_field", Type: "string"})
	defer func() { logicalSchemas[0].Fields = original }()
	if indexVersion() == before {
		t.Fatal("index version did not change with the schema")
	}
}

func TestCollectionSchemasOmitReservedIDField(t *testing.T) {
	for _, schema := range logicalSchemas {
		for _, field := range schema.Fields {
			if field.Name == "id" {
				t.Errorf("%s schema declares reserved id field", schema.Name)
			}
		}
	}
}

func TestIndexEntriesContinuesPastFailures(t *testing.T) {
	ids := []string{"000000000001", "000000000002", "000000000003"}
	bad := ids[1]
	var enqueued []string
	indexed, err := indexEntries(context.Background(), ids,
		func(_ context.Context, id string) error {
			if id == bad {
				return errors.New("boom")
			}
			return nil
		},
		func(id string) error { enqueued = append(enqueued, id); return nil })
	if err != nil || indexed != 2 {
		t.Fatalf("indexed = %d, err = %v; want 2, nil", indexed, err)
	}
	if len(enqueued) != 1 || enqueued[0] != ids[1] {
		t.Fatalf("enqueued = %v, want only the failed entry", enqueued)
	}
}

func TestIndexEntriesFailsWhenEverythingFails(t *testing.T) {
	ids := []string{"000000000001", "000000000002"}
	_, err := indexEntries(context.Background(), ids,
		func(context.Context, string) error { return errors.New("down") },
		func(string) error { return nil })
	if err == nil {
		t.Fatal("expected error when every entry fails")
	}
}

// The community names are loaded as a synonym set before the collection that
// uses it is created.
func TestCreateCollectionsLoadsCommunityNames(t *testing.T) {
	fake := newFakeTypesense()
	client := newFakeClient(t, fake)
	if err := client.createCollections(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.synonymSets, []string{communityNamesSet}) {
		t.Fatalf("synonym sets = %v", fake.synonymSets)
	}
	schema := fake.collections[client.names.Blueprints]
	if !reflect.DeepEqual(schema.SynonymSets, []string{communityNamesSet}) {
		t.Fatalf("collection synonym sets = %v", schema.SynonymSets)
	}
	var steps []string
	for _, request := range fake.requests {
		if strings.HasPrefix(request, "PUT /synonym_sets/") || request == "POST /collections" {
			steps = append(steps, request)
		}
	}
	if want := []string{"PUT /synonym_sets/community-names", "POST /collections"}; !reflect.DeepEqual(steps, want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
}
