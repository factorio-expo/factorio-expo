package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"factorio-expo/apps/expo-api/internal/httpapi"
)

// fakeGitHub is GitHub's token and user endpoints with PKCE: a code is good
// only with the verifier whose challenge it was issued for.
type fakeGitHub struct {
	mu    sync.Mutex
	codes map[string]string // code -> code_challenge
}

// authorize issues a code for the sign-in whose authorization URL the
// browser was sent to, as GitHub does after the person approves.
func (f *fakeGitHub) authorize(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	if err != nil || parsed.Host != "github.com" || parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL %q", authURL)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	code := fmt.Sprintf("code-%d", len(f.codes))
	f.codes[code] = parsed.Query().Get("code_challenge")
	return code
}

func (f *fakeGitHub) client(t *testing.T) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			f.mu.Lock()
			challenge, ok := f.codes[r.PostForm.Get("code")]
			f.mu.Unlock()
			if !ok || oauth2.S256ChallengeFromVerifier(r.PostForm.Get("code_verifier")) != challenge {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"error":"bad_verification_code"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gho_token", "token_type": "bearer"})
		case "/user":
			if r.Header.Get("Authorization") != "Bearer gho_token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"id":583231,"login":"octocat","name":"The Octocat"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	return &http.Client{Transport: toServer{target: target}}
}

// startGitHub starts a GitHub sign-in in a fresh browser and returns the
// response (flow cookie, redirect to GitHub) and its state.
func startGitHub(t *testing.T, handler http.Handler) (*httptest.ResponseRecorder, string) {
	t.Helper()
	started := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/github/start?next=/collections", "", nil))
	authURL, err := url.Parse(started.Header().Get("Location"))
	if started.Code != http.StatusFound || err != nil || cookieNamed(started, "expo_oauth") == nil {
		t.Fatalf("start = %d to %q", started.Code, started.Header().Get("Location"))
	}
	return started, authURL.Query().Get("state")
}

// location is where a response redirects.
func location(recorder *httptest.ResponseRecorder) string { return recorder.Header().Get("Location") }

func githubCallback(handler http.Handler, started *httptest.ResponseRecorder, query url.Values) *httptest.ResponseRecorder {
	callback := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/callback?"+query.Encode(), nil)
	callback.AddCookie(cookieNamed(started, "expo_oauth"))
	return serve(handler, callback)
}

func TestGitHubSignInWithPKCE(t *testing.T) {
	truncateSocialFixtures(t)
	github := &fakeGitHub{codes: map[string]string{}}
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Identities = testIdentities(t, map[string]string{
			"API_GITHUB_CLIENT_ID": "client-id", "API_GITHUB_CLIENT_SECRET": "client-secret",
		}, github.client(t))
	})

	// A code issued for one browser's sign-in, pasted into another
	// browser's callback (with that browser's valid state), fails: the
	// other flow cookie holds another verifier.
	victim, _ := startGitHub(t, handler)
	stolen := github.authorize(t, victim.Header().Get("Location"))
	attacker, attackerState := startGitHub(t, handler)
	injected := githubCallback(handler, attacker, url.Values{"code": {stolen}, "state": {attackerState}})
	if location(injected) != testSite+"/login?error=failed" || sessionToken(injected) != "" ||
		cookieNamed(injected, "expo_signup") != nil {
		t.Fatalf("injected code = %d to %q", injected.Code, location(injected))
	}

	// Declining on GitHub's page is a cancellation.
	declined, declinedState := startGitHub(t, handler)
	cancelled := githubCallback(handler, declined, url.Values{"error": {"access_denied"}, "state": {declinedState}})
	if location(cancelled) != testSite+"/login?error=cancelled" {
		t.Errorf("declined = %q", location(cancelled))
	}

	// The real flow: a new identity goes to /signup with GitHub's login as
	// the suggestion.
	started, state := startGitHub(t, handler)
	code := github.authorize(t, started.Header().Get("Location"))
	callback := githubCallback(handler, started, url.Values{"code": {code}, "state": {state}})
	if location(callback) != testSite+"/signup" || sessionToken(callback) != "" {
		t.Fatalf("callback = %d to %q", callback.Code, location(callback))
	}
	pending := httptest.NewRequest(http.MethodGet, "/api/v1/auth/signup", nil)
	pending.AddCookie(cookieNamed(callback, "expo_signup"))
	recorder := serve(handler, pending)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"suggestion":"octocat"`) ||
		!strings.Contains(recorder.Body.String(), `"provider_label":"GitHub"`) {
		t.Fatalf("pending sign-up = %d: %s", recorder.Code, recorder.Body)
	}
}
