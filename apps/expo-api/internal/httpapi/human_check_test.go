package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
)

// Anonymous uploads, quick previews, revisions and reports (entry and
// problem reports) pass Turnstile
// with their own action; signed-in callers need no token.
func TestAnonymousWritesNeedTheHumanCheck(t *testing.T) {
	truncateSocialFixtures(t)
	// Sign-up needs Turnstile too, so the account is made on a stack without it.
	plain, _ := newTestAPI(t)
	session, _ := registerTestUser(t, plain, "human-check-alice")
	var down atomic.Bool
	verifier := fakeTurnstile(t, &down)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) { deps.Turnstile = verifier })
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	post := func(path, token, sessionToken string, body map[string]string) int {
		t.Helper()
		if token != "" {
			body["turnstile_token"] = token
		}
		return serve(handler, authedRequest(http.MethodPost, path, sessionToken, jsonBody(t, body))).Code
	}
	upload := func() map[string]string { return map[string]string{"blueprint": string(exchange)} }

	expectCode(t, "upload without a token",
		serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", "", jsonBody(t, upload()))),
		http.StatusForbidden, "turnstile_failed")
	if code := post("/api/v1/entries", "pass:quick-preview", "", upload()); code != http.StatusForbidden {
		t.Fatalf("another form's token = %d, want 403", code)
	}
	if code := post("/api/v1/previews", "pass:quick-preview", "", upload()); code != http.StatusCreated {
		t.Fatalf("quick preview = %d", code)
	}
	created := serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", "",
		jsonBody(t, map[string]string{"blueprint": string(exchange), "turnstile_token": "pass:upload"})))
	if created.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", created.Code, created.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	revisions := "/api/v1/entries/" + entry.ID + "/revisions?token=" + entry.EditToken
	if code := post(revisions, "", "", upload()); code != http.StatusForbidden {
		t.Fatalf("anonymous revision without a token = %d", code)
	}
	if code := post(revisions, "pass:revision", "", upload()); code != http.StatusCreated {
		t.Fatalf("anonymous revision = %d", code)
	}
	report := "/api/v1/entries/" + entry.ID + "/report"
	if code := post(report, "", "", map[string]string{"reason": "spam"}); code != http.StatusForbidden {
		t.Fatalf("anonymous report without a token = %d", code)
	}
	if code := post(report, "pass:report", "", map[string]string{"reason": "spam"}); code != http.StatusNoContent {
		t.Fatalf("anonymous report = %d", code)
	}
	problem := map[string]string{"body": "The preview is cut off", "page": "/e/" + entry.ID + "?token=x", "entry_id": entry.ID}
	if code := post("/api/v1/problem-reports", "pass:report", "", problem); code != http.StatusForbidden {
		t.Fatalf("problem report with an entry report's token = %d, want 403", code)
	}
	if code := post("/api/v1/problem-reports", "pass:problem-report", "", problem); code != http.StatusNoContent {
		t.Fatalf("anonymous problem report = %d", code)
	}
	if code := post("/api/v1/problem-reports", "pass:problem-report", "", map[string]string{"body": ""}); code != http.StatusBadRequest {
		t.Fatalf("empty problem report = %d, want 400", code)
	}
	down.Store(true)
	expectCode(t, "upload while siteverify is down",
		serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", "",
			jsonBody(t, map[string]string{"blueprint": string(exchange), "turnstile_token": "pass:upload"}))),
		http.StatusServiceUnavailable, "turnstile_unavailable")

	// Signed in: no human check at all (it is down, too).
	if code := post("/api/v1/entries", "", session, upload()); code != http.StatusCreated {
		t.Fatalf("signed-in upload = %d", code)
	}
	if code := post(report, "", session, map[string]string{"reason": "offensive"}); code != http.StatusNoContent {
		t.Fatalf("signed-in report = %d", code)
	}
	if code := post("/api/v1/problem-reports", "", session, map[string]string{"body": "Search is slow"}); code != http.StatusNoContent {
		t.Fatalf("signed-in problem report = %d", code)
	}
}
