package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/ratelimit"
)

// accountExport is the part of the export document the tests read.
type accountExport struct {
	Format  string
	Version int
	Account struct {
		ID          string
		Username    string
		Status      string
		SocialLinks []struct{ Platform, Handle string } `json:"social_links"`
	}
	Passkeys []map[string]any
	Sessions []map[string]any
	Entries  []struct {
		ID        string
		Revisions []struct {
			Version        int
			ExchangeString string `json:"exchange_string"`
		}
	}
	Comments []struct {
		EntryID string `json:"entry_id"`
		Number  int
		Body    string
	}
	Favorites []struct {
		EntryID string `json:"entry_id"`
	}
	PinnedEntries []struct {
		EntryID string `json:"entry_id"`
	} `json:"pinned_entries"`
	XPEvents []struct {
		Reason  string
		Points  int
		EntryID *string `json:"entry_id"`
	} `json:"xp_events"`
	Collections []struct {
		Name    string
		Entries []struct {
			EntryID string `json:"entry_id"`
		}
	}
	CommentReports []struct{ Reason string } `json:"comment_reports"`
}

func exportRequest(token string) *http.Request {
	return authedRequest(http.MethodGet, "/api/v1/me/export", token, nil)
}

func TestAccountExport(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	device, signUp := signUpWithPasskey(t, handler, "export-alice")
	token := sessionToken(signUp)
	var me struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(signUp.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	bob, _ := registerTestUser(t, handler, "export-bob")

	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	created := serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", token,
		jsonBody(t, map[string]string{"blueprint": string(exchange)})))
	var owned catalog.Entry
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &owned) != nil {
		t.Fatalf("create entry = %d: %s", created.Code, created.Body)
	}
	other := createTestEntry(t, handler)
	comments := "/api/v1/entries/" + other.ID + "/comments"
	for _, step := range []struct {
		token, path string
		body        any
	}{
		{token, comments, map[string]string{"body": "alice was here"}},
		{bob, comments, map[string]string{"body": "bob's own words"}},
		{token, comments + "/2/report", map[string]string{"reason": "spam"}},
		{token, "/api/v1/entries/" + other.ID + "/favorite", nil},
		{token, "/api/v1/collections", map[string]string{"name": "Keepers", "visibility": "private"}},
		{token, "/api/v1/entries/" + owned.ID + "/pin", nil},
	} {
		var body []byte
		if step.body != nil {
			body = jsonBody(t, step.body)
		}
		if code := serve(handler, authedRequest(http.MethodPost, step.path, step.token, body)).Code; code >= 300 {
			t.Fatalf("POST %s = %d", step.path, code)
		}
	}

	profile := serve(handler, authedRequest(http.MethodPatch, "/api/v1/me/profile", token, jsonBody(t, map[string]any{
		"status": "Busy on Gleba", "links": []map[string]string{{"platform": "github", "handle": "alice"}},
	})))
	if profile.Code != http.StatusOK {
		t.Fatalf("PATCH /api/v1/me/profile = %d: %s", profile.Code, profile.Body)
	}

	// An hour after the sign-in the export asks to confirm it's you.
	ageSignIns(t, me.UserID)
	expectReauthRequired(t, "export", serve(handler, exportRequest(token)))
	if confirmed := confirmWithPasskey(t, handler, token, token, device); confirmed.Code != http.StatusNoContent {
		t.Fatalf("confirm = %d: %s", confirmed.Code, confirmed.Body)
	}

	recorder := serve(handler, exportRequest(token))
	if recorder.Code != http.StatusOK {
		t.Fatalf("export = %d: %s", recorder.Code, recorder.Body)
	}
	header := recorder.Header()
	if got := header.Get("Content-Disposition"); !strings.HasPrefix(got, `attachment; filename="factorio-expo-export-alice-`) {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.HasPrefix(header.Get("Content-Type"), "application/json") || header.Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v", header)
	}
	raw := recorder.Body.String()
	if strings.Contains(raw, "bob's own words") || strings.Contains(raw, "credential") ||
		strings.Contains(raw, "public_key") || strings.Contains(raw, "edit_token") {
		t.Errorf("export holds data it must not: %s", raw)
	}
	var export accountExport
	if err := json.Unmarshal(recorder.Body.Bytes(), &export); err != nil {
		t.Fatalf("export is not one JSON document: %v\n%s", err, raw)
	}
	if export.Format != "factorio-expo-account-export" || export.Version != 1 ||
		export.Account.ID != me.UserID || export.Account.Username != "export-alice" {
		t.Errorf("account = %+v", export)
	}
	if export.Account.Status != "Busy on Gleba" || len(export.Account.SocialLinks) != 1 ||
		export.Account.SocialLinks[0].Platform != "github" || export.Account.SocialLinks[0].Handle != "alice" {
		t.Errorf("profile = %+v", export.Account)
	}
	if len(export.PinnedEntries) != 1 || export.PinnedEntries[0].EntryID != owned.ID {
		t.Errorf("pinned entries = %+v, want %s", export.PinnedEntries, owned.ID)
	}
	if len(export.Passkeys) != 1 || export.Passkeys[0]["id"] == "" || len(export.Passkeys[0]) != 3 {
		t.Errorf("passkeys = %v, want one with id and dates only", export.Passkeys)
	}
	if len(export.Sessions) == 0 {
		t.Error("no sessions")
	}
	if len(export.Entries) != 1 || export.Entries[0].ID != owned.ID || len(export.Entries[0].Revisions) != 1 ||
		export.Entries[0].Revisions[0].ExchangeString != string(exchange) {
		t.Errorf("entries = %+v, want the owned entry with its original string", export.Entries)
	}
	if len(export.Comments) != 1 || export.Comments[0].Body != "alice was here" || export.Comments[0].EntryID != other.ID {
		t.Errorf("comments = %+v", export.Comments)
	}
	if len(export.Favorites) != 1 || export.Favorites[0].EntryID != other.ID {
		t.Errorf("favorites = %+v", export.Favorites)
	}
	if len(export.Collections) != 1 || export.Collections[0].Name != "Keepers" {
		t.Errorf("collections = %+v", export.Collections)
	}
	if len(export.CommentReports) != 1 || export.CommentReports[0].Reason != "spam" {
		t.Errorf("comment reports = %+v", export.CommentReports)
	}
	// The upload and the comment earned XP; the ledger is the account's data.
	if len(export.XPEvents) != 2 || export.XPEvents[0].Reason != "upload" || export.XPEvents[0].Points != 10 ||
		export.XPEvents[0].EntryID == nil || *export.XPEvents[0].EntryID != owned.ID || export.XPEvents[1].Reason != "comment" {
		t.Errorf("xp events = %+v", export.XPEvents)
	}

	if code := serve(handler, exportRequest("")).Code; code != http.StatusUnauthorized {
		t.Errorf("signed-out export = %d, want 401", code)
	}
}

// The export has its own small budget per account, drawn only once the
// fresh sign-in check passed.
func TestAccountExportRateLimit(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Limiter = ratelimit.New(1000)
	})
	token, userID := registerTestUser(t, handler, "export-often")
	ageSignIns(t, userID)
	for range 5 {
		expectReauthRequired(t, "stale export", serve(handler, exportRequest(token)))
	}
	execTestSQL(t, "UPDATE user_sessions SET auth_time = now() WHERE user_id = $1", userID)
	for i := range 3 {
		if code := serve(handler, exportRequest(token)).Code; code != http.StatusOK {
			t.Fatalf("export %d = %d, want 200", i+1, code)
		}
	}
	if code := serve(handler, exportRequest(token)).Code; code != http.StatusTooManyRequests {
		t.Fatalf("fourth export = %d, want 429", code)
	}
}
