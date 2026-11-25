package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// truncateSocialFixtures clears users and collections (which cascade to
// entries, favorites, and collection_entries) before a favorites/collections
// test runs, so registering fixed test usernames never collides with a
// previous test run against the same database.
func truncateSocialFixtures(t *testing.T) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		return
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), "TRUNCATE users, collections, ban_holds CASCADE"); err != nil {
		t.Fatalf("truncate users: %v", err)
	}
}

// sessionToken returns the session token a response set as a cookie, or "".
func sessionToken(recorder *httptest.ResponseRecorder) string {
	for _, cookie := range recorder.Result().Cookies() {
		if (cookie.Name == "expo_session" || cookie.Name == "__Host-expo_session") && cookie.MaxAge > 0 {
			return cookie.Value
		}
	}
	return ""
}

func authedRequest(method, path, token string, body []byte) *http.Request {
	var request *http.Request
	if body != nil {
		request = httptest.NewRequest(method, path, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	} else {
		request = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func createTestEntry(t *testing.T, handler http.Handler) catalog.Entry {
	t.Helper()
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	created := postJSON(t, handler, "/api/v1/entries", map[string]string{"blueprint": string(exchange)})
	if created.Code != http.StatusCreated {
		t.Fatalf("create entry status = %d, body = %s", created.Code, created.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	return entry
}

// expectFavoriteState checks what the entry read (is_favorited) and, for a
// signed-in caller, GET /favorite ({"favorited"}) report; anonymous callers get
// false from the entry and 401 from /favorite.
func expectFavoriteState(t *testing.T, handler http.Handler, entryID, token string, want bool) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/entries/"+entryID, token, nil))
	var entry struct {
		IsFavorited *bool `json:"is_favorited"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &entry); err != nil || entry.IsFavorited == nil {
		t.Fatalf("entry read status = %d, body = %s: no is_favorited", recorder.Code, recorder.Body)
	}
	if *entry.IsFavorited != want {
		t.Errorf("entry is_favorited = %v, want %v", *entry.IsFavorited, want)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/entries/"+entryID+"/favorite", token, nil))
	if token == "" {
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("anonymous GET favorite status = %d, want 401", recorder.Code)
		}
		return
	}
	var state struct {
		Favorited *bool `json:"favorited"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &state); err != nil || state.Favorited == nil {
		t.Fatalf("GET favorite status = %d, body = %s", recorder.Code, recorder.Body)
	}
	if *state.Favorited != want {
		t.Errorf("GET favorite = %v, want %v", *state.Favorited, want)
	}
}

func TestFavoritesEndpointsRequireAuthAndRoundTrip(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	entry := createTestEntry(t, handler)

	// Anonymous callers are rejected.
	anon := httptest.NewRequest(http.MethodPost, "/api/v1/entries/"+entry.ID+"/favorite", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, anon)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous favorite status = %d, want 401", recorder.Code)
	}

	token, _ := registerTestUser(t, handler, "fav-alice")

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/entries/"+entry.ID+"/favorite", token, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("favorite status = %d, body = %s", recorder.Code, recorder.Body)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/me/favorites", token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list favorites status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var listed struct {
		Favorites []catalog.EntrySummary `json:"favorites"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode favorites: %v", err)
	}
	if len(listed.Favorites) != 1 || listed.Favorites[0].ID != entry.ID {
		t.Fatalf("favorites = %+v, want exactly entry %s", listed.Favorites, entry.ID)
	}
	expectFavoriteState(t, handler, entry.ID, token, true)
	expectFavoriteState(t, handler, entry.ID, "", false)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodDelete, "/api/v1/entries/"+entry.ID+"/favorite", token, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unfavorite status = %d, body = %s", recorder.Code, recorder.Body)
	}
	expectFavoriteState(t, handler, entry.ID, token, false)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/me/favorites", token, nil))
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode favorites after removal: %v", err)
	}
	if len(listed.Favorites) != 0 {
		t.Fatalf("favorites after removal = %+v, want none", listed.Favorites)
	}
}

func TestCollectionsEndpointsEnforceOwnershipAndVisibility(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	entry := createTestEntry(t, handler)

	ownerToken, _ := registerTestUser(t, handler, "col-owner")
	otherToken, _ := registerTestUser(t, handler, "col-other")

	createBody, _ := json.Marshal(map[string]string{
		"name": "My Collection", "description": "desc", "visibility": "private",
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/collections", ownerToken, createBody))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create collection status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var collection catalog.Collection
	if err := json.Unmarshal(recorder.Body.Bytes(), &collection); err != nil {
		t.Fatalf("decode collection: %v", err)
	}

	// A non-owner cannot read a private collection.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/collections/"+collection.ID, otherToken, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("other user get private collection status = %d, want 404", recorder.Code)
	}

	// A non-owner cannot add entries.
	addBody, _ := json.Marshal(map[string]any{"entry_id": entry.ID})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder,
		authedRequest(http.MethodPost, "/api/v1/collections/"+collection.ID+"/entries", otherToken, addBody))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("other user add entry status = %d, want 403", recorder.Code)
	}

	// The owner adds an entry successfully.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder,
		authedRequest(http.MethodPost, "/api/v1/collections/"+collection.ID+"/entries", ownerToken, addBody))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("owner add entry status = %d, body = %s", recorder.Code, recorder.Body)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/collections/"+collection.ID, ownerToken, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("owner get collection status = %d, body = %s", recorder.Code, recorder.Body)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &collection); err != nil {
		t.Fatalf("decode collection: %v", err)
	}
	if len(collection.Entries) != 1 || collection.Entries[0].ID != entry.ID {
		t.Fatalf("collection entries = %+v, want exactly entry %s", collection.Entries, entry.ID)
	}

	// The owner makes it public; now the other user can read it.
	updateBody, _ := json.Marshal(map[string]string{
		"name": "My Collection", "description": "desc", "visibility": "public",
	})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder,
		authedRequest(http.MethodPatch, "/api/v1/collections/"+collection.ID, ownerToken, updateBody))
	if recorder.Code != http.StatusOK {
		t.Fatalf("update collection status = %d, body = %s", recorder.Code, recorder.Body)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/collections/"+collection.ID, otherToken, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("other user get public collection status = %d, want 200", recorder.Code)
	}

	// A non-owner cannot delete it; the owner can.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodDelete, "/api/v1/collections/"+collection.ID, otherToken, nil))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("other user delete collection status = %d, want 403", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodDelete, "/api/v1/collections/"+collection.ID, ownerToken, nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("owner delete collection status = %d, body = %s", recorder.Code, recorder.Body)
	}
}

func TestEntryCollectionsReportMembership(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	entry := createTestEntry(t, handler)
	token, _ := registerTestUser(t, handler, "picker")
	otherToken, _ := registerTestUser(t, handler, "picker-other")

	create := func(token, name string) string {
		body, _ := json.Marshal(map[string]string{"name": name})
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/collections", token, body))
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create collection status = %d, body = %s", recorder.Code, recorder.Body)
		}
		var collection catalog.Collection
		if err := json.Unmarshal(recorder.Body.Bytes(), &collection); err != nil {
			t.Fatalf("decode collection: %v", err)
		}
		return collection.ID
	}
	holding := create(token, "Holding")
	empty := create(token, "Empty")
	create(otherToken, "Someone else's")
	addBody, _ := json.Marshal(map[string]any{"entry_id": entry.ID})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/collections/"+holding+"/entries", token, addBody))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("add entry status = %d, body = %s", recorder.Code, recorder.Body)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/entries/"+entry.ID+"/collections", token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("entry collections status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var body struct {
		Collections []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Contains *bool  `json:"contains"`
		} `json:"collections"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode entry collections: %v", err)
	}
	got := map[string]bool{}
	for _, collection := range body.Collections {
		if collection.Contains == nil {
			t.Fatalf("collection %s has no contains flag: %s", collection.ID, recorder.Body)
		}
		got[collection.ID] = *collection.Contains
	}
	if len(got) != 2 || !got[holding] || got[empty] {
		t.Fatalf("membership = %v, want only %s holding the entry (of the caller's two)", got, holding)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/entries/"+entry.ID+"/collections", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous entry collections status = %d, want 401", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet,
		"/api/v1/entries/000000000000/collections", token, nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown entry collections status = %d, want 404", recorder.Code)
	}
}

func TestTagsEndpointIsPublicAndCurated(t *testing.T) {
	handler, _ := newTestAPI(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list tags status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var body struct {
		Groups []catalog.TagGroup `json:"groups"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode tags: %v", err)
	}
	if len(body.Groups) == 0 || len(body.Groups[0].Tags) == 0 {
		t.Fatal("expected the curated tag set to be non-empty")
	}
}

func TestPublicProfile(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)

	token, _ := registerTestUser(t, handler, "profile-alice")
	var recorder *httptest.ResponseRecorder

	// Public profile: no auth, name and join date only.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/users/profile-alice", "", nil))
	if recorder.Code != http.StatusOK || bytes.Contains(recorder.Body.Bytes(), []byte("token")) {
		t.Fatalf("profile status = %d, body = %s", recorder.Code, recorder.Body)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/users/nobody-here", "", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown profile status = %d, want 404", recorder.Code)
	}

	// Only public collections are listed on the profile.
	for name, visibility := range map[string]string{"Public one": "public", "Secret one": "private"} {
		body, _ := json.Marshal(map[string]string{"name": name, "visibility": visibility})
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/collections", token, body))
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create collection status = %d, body = %s", recorder.Code, recorder.Body)
		}
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/users/profile-alice/collections", "", nil))
	var listed struct {
		Collections []catalog.Collection `json:"collections"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode collections: %v", err)
	}
	if len(listed.Collections) != 1 || listed.Collections[0].Name != "Public one" {
		t.Fatalf("public collections = %+v, want only \"Public one\"", listed.Collections)
	}

	// Entries: an owned public entry shows up with its uploader's name.
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"blueprint": string(exchange), "title": "Owned"})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodPost, "/api/v1/entries", token, body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create owned entry status = %d, body = %s", recorder.Code, recorder.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(recorder.Body.Bytes(), &entry); err != nil {
		t.Fatalf("decode entry: %v", err)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/entries/"+entry.ID, "", nil))
	if err := json.Unmarshal(recorder.Body.Bytes(), &entry); err != nil || entry.OwnerUsername != "profile-alice" {
		t.Fatalf("entry owner_username = %q (err %v), want profile-alice", entry.OwnerUsername, err)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, authedRequest(http.MethodGet, "/api/v1/users/profile-alice/entries", "", nil))
	var entries struct {
		Entries []catalog.EntrySummary `json:"entries"`
		Total   int64                  `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &entries); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	if entries.Total != 1 || len(entries.Entries) != 1 || entries.Entries[0].Title != "Owned" {
		t.Fatalf("user entries = %+v, want the one owned entry titled Owned", entries)
	}
}
