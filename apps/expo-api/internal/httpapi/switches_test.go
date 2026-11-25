package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/settings"
	"factorio-expo/apps/expo-api/internal/staffops"
)

type pausedBody struct {
	Code string `json:"code"`
}

func TestEmergencySwitches(t *testing.T) {
	truncateSocialFixtures(t)
	var catalogService *catalog.Service
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) { catalogService = deps.Catalog })
	token, _ := registerTestUser(t, handler, "switch-user")
	staff := audit.Staff{Username: "tester"}
	set := func(values map[string]string) {
		t.Helper()
		if _, err := opsFor(catalogService).UpdateSettings(t.Context(), staff, staffops.SettingsUpdate{Values: values}); err != nil {
			t.Fatalf("update settings: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = opsFor(catalogService).UpdateSettings(context.Background(), staff, staffops.SettingsUpdate{Values: map[string]string{
			settings.AllowUploads: "true", settings.AllowAnonymousUploads: "true",
			settings.AllowSignups: "true", settings.AllowComments: "true", settings.SiteBanner: "",
		}})
	})
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	upload := jsonBody(t, map[string]string{"blueprint": string(exchange)})
	post := func(path, bearer string, body []byte) (int, string) {
		recorder := serve(handler, authedRequest(http.MethodPost, path, bearer, body))
		var parsed pausedBody
		_ = json.Unmarshal(recorder.Body.Bytes(), &parsed)
		return recorder.Code, parsed.Code
	}

	set(map[string]string{settings.AllowAnonymousUploads: "false"})
	if status, code := post("/api/v1/entries", "", upload); status != http.StatusServiceUnavailable || code != "anonymous_uploads_paused" {
		t.Fatalf("anonymous upload = %d %q", status, code)
	}
	if status, code := post("/api/v1/previews", "", upload); status != http.StatusServiceUnavailable || code != "anonymous_uploads_paused" {
		t.Fatalf("anonymous quick preview = %d %q", status, code)
	}
	if status, _ := post("/api/v1/entries", token, upload); status != http.StatusCreated {
		t.Fatalf("signed-in upload while only anonymous is paused = %d, want 201", status)
	}

	set(map[string]string{settings.AllowUploads: "false"})
	if status, code := post("/api/v1/entries", token, upload); status != http.StatusServiceUnavailable || code != "uploads_paused" {
		t.Fatalf("signed-in upload = %d %q", status, code)
	}
	if recorder := serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", token, upload)); recorder.Header().Get("Retry-After") == "" {
		t.Fatal("a paused action must say when to retry")
	}

	set(map[string]string{settings.AllowComments: "false", settings.AllowSignups: "false"})
	if status, code := post("/api/v1/entries/abcdefghijkl/comments", token, jsonBody(t, map[string]string{"body": "hi"})); status != http.StatusServiceUnavailable || code != "comments_paused" {
		t.Fatalf("comment = %d %q", status, code)
	}
	if status, code := post("/api/v1/auth/signup", "", jsonBody(t, map[string]string{"username": "someone"})); code == "signups_paused" {
		t.Fatalf("a sign-up under way must not be paused: %d %q", status, code)
	}
	if status, code := post("/api/v1/auth/passkeys/signup/begin", "", jsonBody(t, map[string]string{"username": "someone"})); status != http.StatusServiceUnavailable || code != "signups_paused" {
		t.Fatalf("passkey sign-up = %d %q", status, code)
	}

	set(map[string]string{settings.SiteBanner: "  Back soon  "})
	recorder := serve(handler, authedRequest(http.MethodGet, "/api/v1/site", "", nil))
	var site struct {
		Banner           string `json:"banner"`
		Uploads          bool   `json:"uploads"`
		AnonymousUploads bool   `json:"anonymous_uploads"`
		Signups          bool   `json:"signups"`
		Comments         bool   `json:"comments"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &site); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/site = %d %s", recorder.Code, recorder.Body)
	}
	if site.Banner != "Back soon" || site.Uploads || site.AnonymousUploads || site.Signups || site.Comments {
		t.Fatalf("site status = %+v", site)
	}

	// Reads are never paused.
	if status := serve(handler, authedRequest(http.MethodGet, "/api/v1/me", token, nil)).Code; status != http.StatusOK {
		t.Fatalf("GET /api/v1/me while paused = %d", status)
	}
}

func TestUpdateSettingsValidatesAndAudits(t *testing.T) {
	truncateSocialFixtures(t)
	var catalogService *catalog.Service
	newTestAPIWith(t, func(deps *httpapi.Dependencies) { catalogService = deps.Catalog })
	staff := audit.Staff{Username: "tester"}
	t.Cleanup(func() {
		_, _ = opsFor(catalogService).UpdateSettings(context.Background(), staff, staffops.SettingsUpdate{Values: map[string]string{settings.AllowSignups: "true"}})
	})

	for _, bad := range []map[string]string{
		{"NOPE": "true"},
		{settings.AllowUploads: "maybe"},
		{settings.SiteBanner: "two\nlines"},
	} {
		if _, err := opsFor(catalogService).UpdateSettings(t.Context(), staff, staffops.SettingsUpdate{Values: bad}); err == nil {
			t.Fatalf("update %v succeeded", bad)
		}
	}
	changed, err := opsFor(catalogService).UpdateSettings(t.Context(), staff, staffops.SettingsUpdate{
		Values: map[string]string{settings.AllowSignups: "false", settings.AllowComments: "true"},
		Reason: "flood",
	})
	if err != nil || len(changed) != 1 || changed[0] != settings.AllowSignups {
		t.Fatalf("changed = %v, %v; only the differing key counts", changed, err)
	}
	list, err := opsFor(catalogService).ListSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range list {
		if setting.Key == settings.AllowSignups && (setting.Value != "false" || setting.UpdatedBy != "tester" || setting.UpdatedAt == nil) {
			t.Fatalf("setting = %+v", setting)
		}
	}
}
