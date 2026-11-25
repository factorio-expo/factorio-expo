package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/oauth2"

	"factorio-expo/apps/expo-api/internal/identity"
)

// oauthCase is one OAuth provider as the real one answers.
type oauthCase struct {
	name, idEnv, secretEnv string
	// authURL is the authorization endpoint (scheme, host and path).
	authURL string
	// tokenHost, tokenPath and userHost, userPath are where the exchange
	// and the user-info call must go.
	tokenHost, tokenPath string
	userHost, userPath   string
	scope                string
	userBody             string
	wantID, wantHint     string
}

var oauthCases = []oauthCase{
	{
		name: "github", idEnv: "API_GITHUB_CLIENT_ID", secretEnv: "API_GITHUB_CLIENT_SECRET",
		authURL:   "https://github.com/login/oauth/authorize",
		tokenHost: "github.com", tokenPath: "/login/oauth/access_token",
		userHost: "api.github.com", userPath: "/user",
		userBody: `{"id":583231,"login":"octocat","name":"The Octocat","email":"octo@example.com"}`,
		wantID:   "583231", wantHint: "octocat",
	},
	{
		name: "discord", idEnv: "API_DISCORD_CLIENT_ID", secretEnv: "API_DISCORD_CLIENT_SECRET",
		authURL:   "https://discord.com/oauth2/authorize",
		tokenHost: "discord.com", tokenPath: "/api/oauth2/token",
		userHost: "discord.com", userPath: "/api/users/@me",
		scope:    "identify",
		userBody: `{"id":"80351110224678912","username":"nelly","global_name":"Nelly"}`,
		wantID:   "80351110224678912", wantHint: "nelly",
	},
	{
		name: "google", idEnv: "API_GOOGLE_CLIENT_ID", secretEnv: "API_GOOGLE_CLIENT_SECRET",
		authURL:   "https://accounts.google.com/o/oauth2/auth",
		tokenHost: "oauth2.googleapis.com", tokenPath: "/token",
		userHost: "www.googleapis.com", userPath: "/oauth2/v2/userinfo",
		scope:    "profile",
		userBody: `{"id":"109876543210987654321","name":"Ada Lovelace"}`,
		wantID:   "109876543210987654321", wantHint: "Ada Lovelace",
	},
}

const (
	callbackBase = "https://api.expo.test"
	accessToken  = "access-token-1"
)

// toServer sends every request to one test server, whatever its host,
// naming the host it was meant for in X-Test-Host.
type toServer struct{ target *url.URL }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Test-Host", r.URL.Host)
	r.URL.Scheme, r.URL.Host, r.Host = s.target.Scheme, s.target.Host, s.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// fakeProvider answers c's token exchange for code and verifier only, and
// its user-info call for the issued access token only. It counts the
// exchanges it was asked for.
func fakeProvider(t *testing.T, c oauthCase, code, verifier string) (*identity.Provider, *int) {
	t.Helper()
	exchanges := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Test-Host")
		switch {
		case host == c.tokenHost && r.URL.Path == c.tokenPath && r.Method == http.MethodPost:
			exchanges++
			if err := r.ParseForm(); err != nil {
				t.Errorf("token request form: %v", err)
			}
			form := r.PostForm
			id, secret := form.Get("client_id"), form.Get("client_secret")
			if id != "client-id" || secret != "client-secret" || form.Get("grant_type") != "authorization_code" ||
				form.Get("redirect_uri") != callbackBase+"/api/v1/auth/"+c.name+"/callback" ||
				form.Get("code") != code || form.Get("code_verifier") != verifier {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": accessToken, "token_type": "bearer"})
		case host == c.userHost && r.URL.Path == c.userPath && r.Method == http.MethodGet:
			if r.Header.Get("Authorization") != "Bearer "+accessToken {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(c.userBody))
		default:
			t.Errorf("unexpected request %s %s%s", r.Method, host, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	env := map[string]string{c.idEnv: "client-id", c.secretEnv: "client-secret"}
	registry, err := identity.Load(callbackBase, func(name string) string { return env[name] },
		&http.Client{Transport: toServer{target: target}})
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := registry.Get(c.name)
	if !ok {
		t.Fatalf("%s not enabled", c.name)
	}
	return provider, &exchanges
}

func TestOAuthBeginUsesPKCE(t *testing.T) {
	for _, c := range oauthCases {
		t.Run(c.name, func(t *testing.T) {
			provider, _ := fakeProvider(t, c, "", "")
			authURL, verifier, err := provider.Begin("state-123")
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(authURL)
			if err != nil {
				t.Fatal(err)
			}
			query := parsed.Query()
			if base := parsed.Scheme + "://" + parsed.Host + parsed.Path; base != c.authURL {
				t.Errorf("auth endpoint = %q, want %q", base, c.authURL)
			}
			want := map[string]string{
				"client_id":             "client-id",
				"redirect_uri":          callbackBase + "/api/v1/auth/" + c.name + "/callback",
				"response_type":         "code",
				"state":                 "state-123",
				"scope":                 c.scope,
				"code_challenge_method": "S256",
				"code_challenge":        oauth2.S256ChallengeFromVerifier(verifier),
			}
			for key, value := range want {
				if got := query.Get(key); got != value {
					t.Errorf("%s = %q, want %q", key, got, value)
				}
			}
			if len(verifier) < 43 {
				t.Errorf("verifier %q is too short", verifier)
			}
			if _, again, _ := provider.Begin("state-123"); again == verifier {
				t.Error("two sign-ins share a verifier")
			}
		})
	}
}

func TestOAuthCompleteSendsVerifier(t *testing.T) {
	for _, c := range oauthCases {
		t.Run(c.name, func(t *testing.T) {
			provider, _ := fakeProvider(t, c, "the-code", "the-verifier")
			got, err := provider.Complete(context.Background(), "the-verifier", url.Values{"code": {"the-code"}, "state": {"s"}})
			if err != nil {
				t.Fatal(err)
			}
			want := identity.Identity{Provider: c.name, UserID: c.wantID, Hint: c.wantHint}
			if got != want {
				t.Errorf("identity = %+v, want %+v", got, want)
			}
		})
	}
}

func TestOAuthCompleteRejects(t *testing.T) {
	for _, c := range oauthCases {
		t.Run(c.name, func(t *testing.T) {
			for name, tc := range map[string]struct {
				verifier      string
				params        url.Values
				wantExchanges int
			}{
				// A stolen code pasted into another browser's callback
				// carries that browser's verifier.
				"other verifier": {"another-verifier", url.Values{"code": {"the-code"}}, 1},
				"other code":     {"the-verifier", url.Values{"code": {"stolen-code"}}, 1},
				"denied":         {"the-verifier", url.Values{"error": {"access_denied"}}, 0},
				"no code":        {"the-verifier", url.Values{}, 0},
				"no verifier":    {"", url.Values{"code": {"the-code"}}, 0},
			} {
				provider, exchanges := fakeProvider(t, c, "the-code", "the-verifier")
				_, err := provider.Complete(context.Background(), tc.verifier, tc.params)
				if !errors.Is(err, identity.ErrFailed) {
					t.Errorf("%s: err = %v, want ErrFailed", name, err)
				}
				if *exchanges != tc.wantExchanges {
					t.Errorf("%s: %d token exchanges, want %d", name, *exchanges, tc.wantExchanges)
				}
			}
		})
	}
}

// A user-info answer without an id fails: accounts match by id only.
func TestOAuthCompleteNeedsUserID(t *testing.T) {
	for _, c := range oauthCases {
		t.Run(c.name, func(t *testing.T) {
			c.userBody = `{"login":"octocat","username":"nelly","name":"Ada"}`
			provider, _ := fakeProvider(t, c, "the-code", "the-verifier")
			if _, err := provider.Complete(context.Background(), "the-verifier", url.Values{"code": {"the-code"}}); !errors.Is(err, identity.ErrFailed) {
				t.Errorf("err = %v, want ErrFailed", err)
			}
		})
	}
}
