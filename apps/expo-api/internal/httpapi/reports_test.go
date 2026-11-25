package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/ratelimit"
)

func TestReportEntryEndpoint(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Limiter = ratelimit.New(1000)
	})
	entry := createTestEntry(t, handler) // anonymous, public
	path := "/api/v1/entries/" + entry.ID + "/report"
	report := func(token, remote string, body map[string]string) *httptest.ResponseRecorder {
		request := authedRequest(http.MethodPost, path, token, jsonBody(t, body))
		request.RemoteAddr = remote
		return serve(handler, request)
	}

	if code := report("", "203.0.113.7:1", map[string]string{"reason": "nope"}).Code; code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown reason = %d, want 422", code)
	}
	// The uploader's edit token marks the entry as theirs.
	withToken := authedRequest(http.MethodPost, path+"?token="+entry.EditToken, "", jsonBody(t, map[string]string{"reason": "spam"}))
	if code := serve(handler, withToken).Code; code != http.StatusBadRequest {
		t.Fatalf("own entry = %d, want 400", code)
	}
	// Signed out, three reports per address, then 429; another address and a
	// signed-in user still may.
	for i := range 2 {
		if code := report("", "203.0.113.7:1", map[string]string{"reason": "offensive"}).Code; code != http.StatusNoContent {
			t.Fatalf("anonymous report %d = %d, want 204", i, code)
		}
	}
	limited := report("", "203.0.113.7:1", map[string]string{"reason": "spam"})
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("4th anonymous report = %d, want 429 with Retry-After", limited.Code)
	}
	if code := report("", "203.0.113.8:1", map[string]string{"reason": "spam"}).Code; code != http.StatusNoContent {
		t.Fatalf("another address = %d, want 204", code)
	}
	alice, _ := registerTestUser(t, handler, "report-alice")
	stolen := map[string]string{"reason": "stolen", "note": "https://example.test/original"}
	if code := report(alice, "203.0.113.7:1", stolen).Code; code != http.StatusNoContent {
		t.Fatalf("signed-in report = %d, want 204", code)
	}
	if code := serve(handler, authedRequest(http.MethodPost, "/api/v1/entries/AAAAAAAAAAAA/report", alice,
		jsonBody(t, stolen))).Code; code != http.StatusNotFound {
		t.Fatalf("missing entry = %d, want 404", code)
	}
}

func TestRecordViewAnswersNoStore(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {})
	entry := createTestEntry(t, handler)
	// The beacon answers 204 whether or not the entry exists, never cached.
	for _, id := range []string{entry.ID, "AAAAAAAAAAAA"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/entries/"+id+"/view", nil)
		request.Header.Set("User-Agent", "Mozilla/5.0 (test)")
		recorder := serve(handler, request)
		if recorder.Code != http.StatusNoContent || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("view of %s = %d (Cache-Control %q), want 204 no-store", id, recorder.Code, recorder.Header().Get("Cache-Control"))
		}
	}
}
