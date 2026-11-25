package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/ratelimit"
)

func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// createPrivateEntry uploads the fixture as a private entry of the token's user.
func createPrivateEntry(t *testing.T, handler http.Handler, token string) catalog.Entry {
	t.Helper()
	return createEntryWithVisibility(t, handler, token, "private")
}

// createEntryWithVisibility uploads the fixture as an entry of the token's user.
func createEntryWithVisibility(t *testing.T, handler http.Handler, token, visibility string) catalog.Entry {
	t.Helper()
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"blueprint": string(exchange), "title": "Secret", "visibility": visibility,
	})
	created := serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", token, body))
	if created.Code != http.StatusCreated {
		t.Fatalf("create %s entry status = %d, %s", visibility, created.Code, created.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestPrivateEntryIsOnlyVisibleToItsOwner(t *testing.T) {
	truncateSocialFixtures(t)
	handler, dispatcher := newTestAPI(t)
	ownerToken, _ := registerTestUser(t, handler, "private-owner")
	otherToken, _ := registerTestUser(t, handler, "private-other")
	entry := createPrivateEntry(t, handler, ownerToken)
	pollUntilImported(t, dispatcher)

	leafID := entry.Revision.Root.Leaves()[0].ID
	base := "/api/v1/entries/" + entry.ID
	paths := []string{
		base,
		base + "/permissions",
		base + "/revisions",
		base + "/revisions/" + entry.Revision.ID,
		base + "/revisions/" + entry.Revision.ID + "/export",
		base + "/blueprints/" + leafID,
		catalog.PreviewURL(leafID),
		catalog.ThumbnailURL(leafID),
	}
	for _, path := range paths {
		for name, token := range map[string]string{"anonymous": "", "other user": otherToken} {
			if code := serve(handler, authedRequest(http.MethodGet, path, token, nil)).Code; code != http.StatusNotFound {
				t.Errorf("%s GET %s = %d, want 404", name, path, code)
			}
		}
		owner := serve(handler, authedRequest(http.MethodGet, path, ownerToken, nil))
		if owner.Code != http.StatusOK {
			t.Errorf("owner GET %s = %d, want 200", path, owner.Code)
		}
		if strings.HasSuffix(path, ".webp") && owner.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("private preview Cache-Control = %q", owner.Header().Get("Cache-Control"))
		}
	}

	// Writes by another user do not reveal that the entry exists either.
	patch := authedRequest(http.MethodPatch, base, otherToken, []byte(`{"title":"pwned"}`))
	if code := serve(handler, patch).Code; code != http.StatusNotFound {
		t.Errorf("other user PATCH = %d, want 404", code)
	}

	// Another user can neither favorite it nor save it into a collection.
	if code := serve(handler, authedRequest(http.MethodPost, base+"/favorite", otherToken, nil)).Code; code != http.StatusNotFound {
		t.Errorf("favorite someone else's private entry = %d, want 404", code)
	}
	collection := serve(handler, authedRequest(http.MethodPost, "/api/v1/collections", otherToken,
		[]byte(`{"name":"mine","visibility":"public"}`)))
	var created catalog.Collection
	_ = json.Unmarshal(collection.Body.Bytes(), &created)
	add := authedRequest(http.MethodPost, "/api/v1/collections/"+created.ID+"/entries", otherToken,
		[]byte(`{"entry_id":"`+entry.ID+`"}`))
	if code := serve(handler, add).Code; code != http.StatusNotFound {
		t.Errorf("collect someone else's private entry = %d, want 404", code)
	}
}

// An unlisted entry is readable by anyone with its link, editable only by its
// owner, and never listed on its owner's public profile.
func TestUnlistedEntryIsViewableByLinkOnly(t *testing.T) {
	truncateSocialFixtures(t)
	handler, dispatcher := newTestAPI(t)
	ownerToken, _ := registerTestUser(t, handler, "unlisted-owner")
	otherToken, _ := registerTestUser(t, handler, "unlisted-other")
	entry := createEntryWithVisibility(t, handler, ownerToken, "unlisted")
	pollUntilImported(t, dispatcher)
	if entry.Visibility != "unlisted" || entry.EditToken != "" {
		t.Fatalf("created entry visibility = %q, edit token %q", entry.Visibility, entry.EditToken)
	}

	leafID := entry.Revision.Root.Leaves()[0].ID
	base := "/api/v1/entries/" + entry.ID
	paths := []string{
		base,
		base + "/revisions",
		base + "/revisions/" + entry.Revision.ID + "/export",
		base + "/blueprints/" + leafID,
		catalog.PreviewURL(leafID),
		catalog.ThumbnailURL(leafID),
	}
	for _, path := range paths {
		for name, token := range map[string]string{"anonymous": "", "other user": otherToken, "owner": ownerToken} {
			response := serve(handler, authedRequest(http.MethodGet, path, token, nil))
			if response.Code != http.StatusOK {
				t.Errorf("%s GET %s = %d, want 200", name, path, response.Code)
			}
			// Anyone with the link may see it, so shared caches may keep it.
			if strings.HasSuffix(path, ".webp") && response.Header().Get("Cache-Control") != "public, max-age=86400" {
				t.Errorf("unlisted preview Cache-Control = %q", response.Header().Get("Cache-Control"))
			}
		}
	}

	// Viewers who are not the owner may not edit or delete it.
	for name, token := range map[string]string{"anonymous": "", "other user": otherToken} {
		permissions := serve(handler, authedRequest(http.MethodGet, base+"/permissions", token, nil))
		if permissions.Code != http.StatusOK || !strings.Contains(permissions.Body.String(), `"can_edit":false`) {
			t.Errorf("%s permissions = %d %s, want can_edit false", name, permissions.Code, permissions.Body)
		}
		patch := authedRequest(http.MethodPatch, base, token, []byte(`{"title":"pwned"}`))
		if code := serve(handler, patch).Code; code != http.StatusForbidden {
			t.Errorf("%s PATCH = %d, want 403", name, code)
		}
		if code := serve(handler, authedRequest(http.MethodDelete, base, token, nil)).Code; code != http.StatusForbidden {
			t.Errorf("%s DELETE = %d, want 403", name, code)
		}
	}

	// Its owner's public profile does not list it, for anyone.
	for name, token := range map[string]string{"anonymous": "", "other user": otherToken, "owner": ownerToken} {
		listing := serve(handler, authedRequest(http.MethodGet, "/api/v1/users/unlisted-owner/entries", token, nil))
		if listing.Code != http.StatusOK || strings.Contains(listing.Body.String(), entry.ID) {
			t.Errorf("%s profile listing = %d %s, want it without the unlisted entry", name, listing.Code, listing.Body)
		}
	}

	if code := serve(handler, authedRequest(http.MethodDelete, base, ownerToken, nil)).Code; code != http.StatusNoContent {
		t.Errorf("owner DELETE = %d, want 204", code)
	}
}

// Anonymous uploads are always public: unlisted and private need an account.
func TestAnonymousUploadMustBePublic(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, visibility := range []string{"unlisted", "private"} {
		body, _ := json.Marshal(map[string]string{"blueprint": string(exchange), "visibility": visibility})
		if code := serve(handler, authedRequest(http.MethodPost, "/api/v1/entries", "", body)).Code; code != http.StatusForbidden {
			t.Errorf("anonymous %s upload = %d, want 403", visibility, code)
		}
	}
}

func TestSessionCookieAuthAndOriginCheck(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.CookieSecure = true
	})
	_, register := signUpWithPasskey(t, handler, "cookie-user")
	var cookie *http.Cookie
	for _, c := range register.Result().Cookies() {
		if c.Name == "__Host-expo_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" ||
		cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v, want HttpOnly Secure SameSite=Lax __Host- cookie", cookie)
	}

	withCookie := func(method, path, body string) *http.Request {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		return request
	}
	if code := serve(handler, withCookie(http.MethodGet, "/api/v1/me", "")).Code; code != http.StatusOK {
		t.Fatalf("GET /api/v1/me with cookie = %d, want 200", code)
	}

	create := `{"name":"c"}`
	crossSite := withCookie(http.MethodPost, "/api/v1/collections", create)
	crossSite.Header.Set("Origin", "https://evil.example")
	if code := serve(handler, crossSite).Code; code != http.StatusForbidden {
		t.Errorf("cross-origin cookie POST = %d, want 403", code)
	}
	fetchSite := withCookie(http.MethodPost, "/api/v1/collections", create)
	fetchSite.Header.Set("Sec-Fetch-Site", "cross-site")
	if code := serve(handler, fetchSite).Code; code != http.StatusForbidden {
		t.Errorf("Sec-Fetch-Site cross-site POST = %d, want 403", code)
	}
	sameOrigin := withCookie(http.MethodPost, "/api/v1/collections", create)
	sameOrigin.Header.Set("Origin", "http://localhost:3000")
	if code := serve(handler, sameOrigin).Code; code != http.StatusCreated {
		t.Errorf("allowed-origin cookie POST = %d, want 201", code)
	}

	logout := serve(handler, withCookie(http.MethodPost, "/api/v1/auth/logout", ""))
	cleared := false
	for _, c := range logout.Result().Cookies() {
		cleared = cleared || (c.Name == cookie.Name && c.MaxAge < 0)
	}
	if logout.Code != http.StatusNoContent || !cleared {
		t.Errorf("logout = %d, cookie cleared = %v", logout.Code, cleared)
	}

	// A stale cookie on a public read is dropped, not an error.
	stale := httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil)
	stale.AddCookie(&http.Cookie{Name: cookie.Name, Value: "not-a-token"})
	if code := serve(handler, stale).Code; code != http.StatusOK {
		t.Errorf("public read with stale cookie = %d, want 200", code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler, _ := newTestAPI(t)
	response := serve(handler, httptest.NewRequest(http.MethodGet, "/api/v1/tags", nil))
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "private, no-cache",
	} {
		if got := response.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestAnonymousUploadsAreRateLimited(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Limiter = ratelimit.New(1000)
	})
	exchange, err := os.ReadFile("../../../../test-data/bp-starter-smelter.txt")
	if err != nil {
		t.Fatal(err)
	}
	upload := func(remote string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"blueprint": string(exchange)})
		request := authedRequest(http.MethodPost, "/api/v1/entries", "", body)
		request.RemoteAddr = remote
		return serve(handler, request)
	}
	for i := range 3 {
		if code := upload("203.0.113.7:1234").Code; code != http.StatusCreated {
			t.Fatalf("upload %d = %d, want 201", i, code)
		}
	}
	limited := upload("203.0.113.7:1234")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("4th anonymous upload = %d, want 429 with Retry-After", limited.Code)
	}
	if code := upload("203.0.113.8:1234").Code; code != http.StatusCreated {
		t.Fatalf("another client's upload = %d, want 201", code)
	}

	// An untrusted peer cannot dodge the limit by naming another client.
	spoofed := authedRequest(http.MethodPost, "/api/v1/entries", "", []byte(`{"blueprint":"x"}`))
	spoofed.RemoteAddr = "203.0.113.7:1234"
	spoofed.Header.Set("X-Real-IP", "198.51.100.1")
	if code := serve(handler, spoofed).Code; code != http.StatusTooManyRequests {
		t.Fatalf("spoofed X-Real-IP upload = %d, want 429", code)
	}
}

func TestSignInRateLimit(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Limiter = ratelimit.New(1000)
	})
	begin := func(remote string) int {
		request := authedRequest(http.MethodPost, "/api/v1/auth/passkeys/login/begin", "", nil)
		request.RemoteAddr = remote
		return serve(handler, request).Code
	}
	for i := range 20 {
		if code := begin("198.51.100.1:1"); code != http.StatusOK {
			t.Fatalf("sign-in step %d = %d, want 200", i+1, code)
		}
	}
	if code := begin("198.51.100.1:1"); code != http.StatusTooManyRequests {
		t.Fatalf("21st sign-in step from one address = %d, want 429", code)
	}
	if code := begin("198.51.100.2:1"); code != http.StatusOK {
		t.Fatalf("another address = %d, want 200", code)
	}
}

func TestLogoutEndsOnlyThisSession(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	device, signUp := signUpWithPasskey(t, handler, "two-devices")
	first := sessionToken(signUp)
	login := signInWithPasskey(t, handler, device)
	second := sessionToken(login)
	if login.Code != http.StatusOK || second == "" {
		t.Fatalf("second login = %d", login.Code)
	}
	me := func(token string) int {
		return serve(handler, authedRequest(http.MethodGet, "/api/v1/me", token, nil)).Code
	}
	if me(first) != http.StatusOK || me(second) != http.StatusOK {
		t.Fatal("both sessions must work before logout")
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(&http.Cookie{Name: "expo_session", Value: first})
	if code := serve(handler, logout).Code; code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", code)
	}
	if me(first) != http.StatusUnauthorized || me(second) != http.StatusOK {
		t.Errorf("after logout /api/v1/me = %d, %d; want 401 for the first session only", me(first), me(second))
	}

	// A logout with a dead token still succeeds and clears the cookie.
	again := serve(handler, authedRequest(http.MethodPost, "/api/v1/auth/logout", first, nil))
	if again.Code != http.StatusNoContent || !clearsCookie(again, "expo_session") {
		t.Errorf("logout with a revoked token = %d", again.Code)
	}
}
