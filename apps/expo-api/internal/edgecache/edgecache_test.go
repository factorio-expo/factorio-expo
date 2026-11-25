package edgecache

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

func TestDisabledPurgerDoesNothing(t *testing.T) {
	purger := New("https://expo.test", "", "zone")
	if purger.Enabled() {
		t.Fatal("a purger without a token must be disabled")
	}
	// A nil purger is safe to call: no panic, nothing sent.
	purger.PurgeEntry("e", []string{"r"}, []string{"abc"})
	var zero Purger
	zero.PurgeEntry("e", nil, []string{"abc"})
}

func TestPurgeSendsEntryURLsInBatches(t *testing.T) {
	var mu sync.Mutex
	var batches [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/zones/zone1/purge_cache" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		var body struct {
			Files []string `json:"files"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		batches = append(batches, body.Files)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	purger := &Purger{
		origin: "https://expo.test", zoneID: "zone1", token: "secret",
		apiBase: server.URL, client: server.Client(),
	}
	ids := make([]string, 13) // 1 + 1 + 13*3 = 41 URLs: two batches of at most 30
	for i := range ids {
		ids[i] = "bp" + strconv.Itoa(i)
	}
	files := purger.entryURLs("entry1", []string{"rev1"}, ids)
	want := []string{
		"https://expo.test/api/v1/entries/entry1/raw",
		"https://expo.test/api/v1/entries/entry1/revisions/rev1/raw",
		"https://expo.test/api/v1/blueprints/bp0/preview.webp",
		"https://expo.test/api/v1/blueprints/bp0/thumbnail.webp",
		"https://expo.test/api/v1/blueprints/bp0/raw",
	}
	for i, want := range want {
		if files[i] != want {
			t.Fatalf("url %d = %s, want %s", i, files[i], want)
		}
	}
	if err := purger.purge(context.Background(), files); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0]) != 30 || len(batches[1]) != 11 {
		t.Fatalf("batches = %d (%d, %d)", len(batches), len(batches[0]), len(batches[len(batches)-1]))
	}
}

func TestPurgeReportsCloudflareErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad token", http.StatusForbidden)
	}))
	defer server.Close()
	purger := &Purger{origin: "https://expo.test", zoneID: "z", token: "t", apiBase: server.URL, client: server.Client()}
	if err := purger.purge(context.Background(), []string{"https://expo.test/x"}); err == nil {
		t.Fatal("a 403 must be an error")
	}
}
