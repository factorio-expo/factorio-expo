package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/preview"
	"factorio-expo/apps/expo-api/internal/progress"
)

// A full file the storage budget evicted answers 404 (never cached), the
// leaf says it is gone, and POST .../preview/render queues a re-render that
// makes the leaf pending while it keeps showing the stored preview.
func TestEvictedFullPreviewAsksForARender(t *testing.T) {
	handler, dispatcher := newTestAPI(t)
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	created := postJSON(t, handler, "/api/v1/entries", map[string]string{"blueprint": string(exchange)})
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	pollUntilImported(t, dispatcher)
	leafID := entry.Revision.Root.Leaves()[0].ID

	serve := func(method, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder
	}
	renderPath := "/api/v1/blueprints/" + leafID + "/preview/render"
	state := func(recorder *httptest.ResponseRecorder) preview.FullRender {
		t.Helper()
		var body preview.FullRender
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", recorder.Body, err)
		}
		return body
	}
	// The stub preview is small: its thumbnail is its full image.
	if got := serve(http.MethodPost, renderPath); got.Code != http.StatusOK ||
		state(got) != (preview.FullRender{Status: progress.StatusReady, FullAvailable: true}) {
		t.Fatalf("small preview: %d %s", got.Code, got.Body)
	}

	// Make it a large preview whose full file is gone.
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(),
		"UPDATE previews SET width = 1600, source_sha256 = $2 WHERE blueprint_id = $1",
		leafID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}

	full := serve(http.MethodGet, catalog.PreviewURL(leafID))
	if full.Code != http.StatusNotFound || full.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("evicted full file: %d, cache %q", full.Code, full.Header().Get("Cache-Control"))
	}
	leaf := func() catalog.LeafDetail {
		t.Helper()
		var detail catalog.LeafDetail
		got := serve(http.MethodGet, "/api/v1/entries/"+entry.ID+"/blueprints/"+leafID)
		if err := json.Unmarshal(got.Body.Bytes(), &detail); err != nil || detail.Preview == nil {
			t.Fatalf("leaf = %s, %v", got.Body, err)
		}
		return detail
	}
	if detail := leaf(); detail.Preview.FullAvailable || detail.PreviewStatus != progress.StatusReady {
		t.Fatalf("evicted leaf = %+v", detail)
	}
	if got := serve(http.MethodPost, renderPath); got.Code != http.StatusAccepted ||
		state(got) != (preview.FullRender{Status: progress.StatusPending}) {
		t.Fatalf("render: %d %s", got.Code, got.Body)
	}
	if got := serve(http.MethodPost, renderPath); got.Code != http.StatusOK || state(got).Status != progress.StatusPending {
		t.Fatalf("render again: %d %s", got.Code, got.Body)
	}
	// Pending, but the stored preview is still shown.
	if detail := leaf(); detail.PreviewStatus != progress.StatusPending || detail.Preview.ThumbnailURL == "" {
		t.Fatalf("re-rendering leaf = %+v", detail)
	}
	if got := serve(http.MethodPost, "/api/v1/blueprints/zzzzzzzzzzzz/preview/render"); got.Code != http.StatusNotFound {
		t.Fatalf("unknown blueprint: %d", got.Code)
	}
}
