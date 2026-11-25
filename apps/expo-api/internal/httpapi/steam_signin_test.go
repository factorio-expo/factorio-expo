package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
)

const steamLogin = "https://steamcommunity.com/openid/login"

// toServer sends every request to one test server, whatever its host, so
// the real Steam provider talks to fakeSteam.
type toServer struct{ target *url.URL }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host, r.Host = s.target.Scheme, s.target.Host, s.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// fakeSteam verifies every assertion and names every player "Factorio Fan".
func fakeSteam(t *testing.T) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openid/login" {
			fmt.Fprint(w, "ns:http://specs.openid.net/auth/2.0\nis_valid:true\n")
			return
		}
		id := r.URL.Query().Get("steamids")
		fmt.Fprintf(w, `{"response":{"players":[{"steamid":%q,"personaname":"Factorio Fan"}]}}`, id)
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	return &http.Client{Transport: toServer{target: target}}
}

// steamSignIn starts a Steam sign-in and comes back as Steam would for
// steamID. It returns the callback's response.
func steamSignIn(t *testing.T, handler http.Handler, start, token, steamID string) *httptest.ResponseRecorder {
	t.Helper()
	return steamCallback(t, handler, serve(handler, authedRequest(http.MethodGet, start, token, nil)), token, steamID)
}

// steamCallback comes back from the Steam sign-in started answered as Steam
// would for steamID, carrying started's flow cookie.
func steamCallback(t *testing.T, handler http.Handler, started *httptest.ResponseRecorder, token, steamID string) *httptest.ResponseRecorder {
	t.Helper()
	authURL, err := url.Parse(started.Header().Get("Location"))
	if started.Code != http.StatusFound || err != nil || !strings.HasPrefix(authURL.String(), steamLogin+"?") {
		t.Fatalf("start = %d to %q", started.Code, started.Header().Get("Location"))
	}
	returnTo, _ := url.Parse(authURL.Query().Get("openid.return_to"))
	claimed := "https://steamcommunity.com/openid/id/" + steamID
	query := returnTo.Query()
	for key, value := range map[string]string{
		"openid.ns": "http://specs.openid.net/auth/2.0", "openid.mode": "id_res",
		"openid.op_endpoint": steamLogin, "openid.claimed_id": claimed, "openid.identity": claimed,
		"openid.return_to": returnTo.String(), "openid.response_nonce": "2026-11-03T10:00:00Z" + steamID,
		"openid.assoc_handle": "1234567890", "openid.sig": "c2ln",
		"openid.signed": "signed,op_endpoint,claimed_id,identity,return_to,response_nonce,assoc_handle",
	} {
		query.Set(key, value)
	}
	callback := authedRequest(http.MethodGet, returnTo.Path+"?"+query.Encode(), token, nil)
	callback.AddCookie(cookieNamed(started, "expo_oauth"))
	return serve(handler, callback)
}

// clearsCookie reports whether a response expires the cookie name (plain or
// __Host- prefixed).
func clearsCookie(recorder *httptest.ResponseRecorder, name string) bool {
	for _, cookie := range recorder.Result().Cookies() {
		if (cookie.Name == name || cookie.Name == "__Host-"+name) && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

func TestSteamSignUpSignInAndLink(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Identities = testIdentities(t, map[string]string{"API_STEAM_API_KEY": "web-api-key"}, fakeSteam(t))
	})

	// A new Steam account goes to /signup, which suggests its persona name.
	callback := steamSignIn(t, handler, "/api/v1/auth/steam/start?next=/collections", "", "76561197960287930")
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != testSite+"/signup" {
		t.Fatalf("new identity callback = %d to %q", callback.Code, callback.Header().Get("Location"))
	}
	if sessionToken(callback) != "" {
		t.Fatal("a new identity must not be signed in before choosing a username")
	}
	signup := cookieNamed(callback, "expo_signup")
	if signup == nil || signup.MaxAge != 600 {
		t.Fatalf("sign-up cookie = %+v, want one for 10 minutes", signup)
	}
	pending := httptest.NewRequest(http.MethodGet, "/api/v1/auth/signup", nil)
	pending.AddCookie(signup)
	recorder := serve(handler, pending)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"suggestion":"Factorio_Fan"`) ||
		!strings.Contains(recorder.Body.String(), `"provider_label":"Steam"`) {
		t.Fatalf("pending sign-up = %d: %s", recorder.Code, recorder.Body)
	}
	body, _ := json.Marshal(map[string]string{"username": "Factorio_Fan"})
	created := postWith(handler, "/api/v1/auth/signup", "", signup, body)
	if created.Code != http.StatusOK || sessionToken(created) == "" ||
		!strings.Contains(created.Body.String(), `"next":"/collections"`) {
		t.Fatalf("complete sign-up = %d: %s", created.Code, created.Body)
	}
	if code := recoveryCodeOf(t, created); !recoveryCodeShape.MatchString(code) {
		t.Fatalf("the provider sign-up's recovery code = %q", code)
	}

	// The same Steam account now signs straight in and lands on next.
	again := steamSignIn(t, handler, "/api/v1/auth/steam/start?next=/collections", "", "76561197960287930")
	if again.Code != http.StatusFound || again.Header().Get("Location") != testSite+"/collections" || sessionToken(again) == "" {
		t.Fatalf("known identity callback = %d to %q", again.Code, again.Header().Get("Location"))
	}

	// Another account cannot link a Steam account that is already taken,
	// but can link its own.
	bob, _ := registerTestUser(t, handler, "steam-bob")
	taken := steamSignIn(t, handler, "/api/v1/auth/steam/start?link=1", bob, "76561197960287930")
	if taken.Header().Get("Location") != testSite+"/account?error=identity-taken" {
		t.Errorf("linking a taken identity went to %q", taken.Header().Get("Location"))
	}
	linked := steamSignIn(t, handler, "/api/v1/auth/steam/start?link=1", bob, "76561197960287931")
	if linked.Header().Get("Location") != testSite+"/account?linked=steam" {
		t.Fatalf("linking went to %q", linked.Header().Get("Location"))
	}
	if providers, _ := signInMethods(t, handler, bob); len(providers) != 1 || providers[0] != "steam" {
		t.Errorf("bob's providers = %v, want [steam]", providers)
	}

	// A callback that does not match the flow (forged state, another
	// provider's callback) is refused and leaves the flow cookie, so the
	// real callback still completes.
	started := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/steam/start", "", nil))
	authURL, _ := url.Parse(started.Header().Get("Location"))
	returnTo, _ := url.Parse(authURL.Query().Get("openid.return_to"))
	for _, path := range []string{
		"/api/v1/auth/steam/callback?state=forged",
		"/api/v1/auth/github/callback?" + returnTo.RawQuery,
	} {
		forged := httptest.NewRequest(http.MethodGet, path, nil)
		forged.AddCookie(cookieNamed(started, "expo_oauth"))
		refused := serve(handler, forged)
		if location := refused.Header().Get("Location"); location != testSite+"/login?error=expired" {
			t.Errorf("%s went to %q", path, location)
		}
		if clearsCookie(refused, "expo_oauth") {
			t.Errorf("%s cleared the flow cookie", path)
		}
	}
	finished := steamCallback(t, handler, started, "", "76561197960287930")
	if finished.Header().Get("Location") != testSite+"/" || sessionToken(finished) == "" {
		t.Errorf("callback after forged ones = %d to %q", finished.Code, finished.Header().Get("Location"))
	}
	if !clearsCookie(finished, "expo_oauth") {
		t.Error("a completed callback left the flow cookie")
	}
}

// A pending sign-up is dropped when any new sign-in starts and on logout.
func TestNewSignInDropsPendingSignup(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Identities = testIdentities(t, map[string]string{"API_STEAM_API_KEY": "web-api-key"}, fakeSteam(t))
	})
	username, _ := json.Marshal(map[string]string{"username": "fresh-start"})
	for name, recorder := range map[string]*httptest.ResponseRecorder{
		"provider start": serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/steam/start", "", nil)),
		"provider link":  serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/steam/start?link=1", "", nil)),
		"passkey login":  postWith(handler, "/api/v1/auth/passkeys/login/begin", "", nil, nil),
		"passkey signup": postWith(handler, "/api/v1/auth/passkeys/signup/begin", "", nil, username),
		"logout":         postWith(handler, "/api/v1/auth/logout", "", nil, nil),
	} {
		if !clearsCookie(recorder, "expo_signup") {
			t.Errorf("%s (%d) kept the pending sign-up", name, recorder.Code)
		}
	}
}

// Deleting an account deletes its sign-in methods: its Steam account signs
// up anew at once, while the username stays held until the hard delete.
func TestDeletedAccountSignsUpAnew(t *testing.T) {
	truncateSocialFixtures(t)
	var service *catalog.Service
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Identities = testIdentities(t, map[string]string{"API_STEAM_API_KEY": "web-api-key"}, fakeSteam(t))
		service = deps.Catalog
	})
	const steamID = "76561197960287940"
	callback := steamSignIn(t, handler, "/api/v1/auth/steam/start", "", steamID)
	body, _ := json.Marshal(map[string]string{"username": "Gone_Soon"})
	created := postWith(handler, "/api/v1/auth/signup", "", cookieNamed(callback, "expo_signup"), body)
	var account struct {
		UserID string `json:"user_id"`
	}
	if created.Code != http.StatusOK || json.Unmarshal(created.Body.Bytes(), &account) != nil {
		t.Fatalf("sign-up = %d: %s", created.Code, created.Body)
	}
	if err := opsFor(service).StaffDeleteUser(context.Background(), audit.Staff{Username: "staff"}, account.UserID, "spam"); err != nil {
		t.Fatal(err)
	}

	again := steamSignIn(t, handler, "/api/v1/auth/steam/start", "", steamID)
	if again.Header().Get("Location") != testSite+"/signup" || sessionToken(again) != "" {
		t.Fatalf("deleted account's Steam sign-in went to %q", again.Header().Get("Location"))
	}
	signup := cookieNamed(again, "expo_signup")
	if held := postWith(handler, "/api/v1/auth/signup", "", signup, body); held.Code != http.StatusConflict {
		t.Errorf("signing up with the deleted account's username = %d, want 409", held.Code)
	}
	body, _ = json.Marshal(map[string]string{"username": "Back_Again"})
	if anew := postWith(handler, "/api/v1/auth/signup", "", signup, body); anew.Code != http.StatusOK || sessionToken(anew) == "" {
		t.Fatalf("signing up anew = %d: %s", anew.Code, anew.Body)
	}
}
