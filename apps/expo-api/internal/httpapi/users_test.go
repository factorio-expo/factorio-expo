package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// The caller's own listing includes private entries, and needs a session.
func TestMyEntriesListsPrivateEntries(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	token, _ := registerTestUser(t, handler, "mine-alice")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/me/entries", "", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous my entries status = %d, want 401", recorder.Code)
	}

	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"blueprint": string(exchange), "title": "Hidden", "visibility": "private"})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/entries", token, body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create private entry status = %d, body = %s", recorder.Code, recorder.Body)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/me/entries", token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("my entries status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var page struct {
		Entries []catalog.EntrySummary `json:"entries"`
		Total   int64                  `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode my entries: %v", err)
	}
	if page.Total != 1 || len(page.Entries) != 1 || page.Entries[0].Title != "Hidden" {
		t.Fatalf("my entries = %+v, want the one private entry titled Hidden", page)
	}
}
