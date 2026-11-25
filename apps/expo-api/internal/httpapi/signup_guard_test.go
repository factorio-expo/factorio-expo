package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/ratelimit"
	"factorio-expo/apps/expo-api/internal/turnstile"
)

// fakeTurnstile is siteverify passing tokens "pass:<action>" for that action
// on the test site's host name; while down is set it answers 500.
func fakeTurnstile(t *testing.T, down *atomic.Bool) *turnstile.Verifier {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		_ = r.ParseForm()
		action, ok := strings.CutPrefix(r.PostForm.Get("response"), "pass:")
		if !ok {
			fmt.Fprint(w, `{"success":false,"error-codes":["invalid-input-response"]}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "action": action, "hostname": "localhost"})
	}))
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	return turnstile.New("secret", "localhost", &http.Client{Transport: toServer{target: target}})
}

// beginPasskeySignup posts the passkey sign-up begin from remote.
func beginPasskeySignup(handler http.Handler, username, token, remote string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"username": username, "turnstile_token": token})
	request := authedRequest(http.MethodPost, "/api/v1/auth/passkeys/signup/begin", "", body)
	request.RemoteAddr = remote
	return serve(handler, request)
}

// passkeySignupFrom runs a whole passkey sign-up from remote and returns the
// finish response.
func passkeySignupFrom(t *testing.T, handler http.Handler, username, token, remote string) *httptest.ResponseRecorder {
	t.Helper()
	begin := beginPasskeySignup(handler, username, token, remote)
	_, finish := createPasskey(t, begin, nil, func(cookie *http.Cookie, answer []byte) *httptest.ResponseRecorder {
		request := authedRequest(http.MethodPost, "/api/v1/auth/passkeys/signup/finish", "", answer)
		request.RemoteAddr = remote
		request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		return serve(handler, request)
	})
	return finish
}

func expectCode(t *testing.T, name string, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var body struct{ Code string }
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != status || body.Code != code {
		t.Errorf("%s = %d %s, want %d %s", name, recorder.Code, recorder.Body, status, code)
	}
}

func TestSignUpHumanCheck(t *testing.T) {
	truncateSocialFixtures(t)
	var down atomic.Bool
	verifier := fakeTurnstile(t, &down)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Turnstile = verifier
		deps.Identities = testIdentities(t, map[string]string{"API_STEAM_API_KEY": "web-api-key"}, fakeSteam(t))
	})
	const remote = "192.0.2.1:1234"

	expectCode(t, "passkey begin without a token",
		beginPasskeySignup(handler, "human", "", remote), http.StatusForbidden, "turnstile_failed")
	expectCode(t, "passkey begin with the provider form's token",
		beginPasskeySignup(handler, "human", "pass:signup-provider", remote), http.StatusForbidden, "turnstile_failed")
	if finish := passkeySignupFrom(t, handler, "human", "pass:signup-passkey", remote); finish.Code != http.StatusOK {
		t.Fatalf("passkey sign-up with a good token = %d: %s", finish.Code, finish.Body)
	}
	// Username checks come first: a taken name never reaches siteverify.
	if code := beginPasskeySignup(handler, "HUMAN", "", remote).Code; code != http.StatusConflict {
		t.Errorf("taken username without a token = %d, want 409", code)
	}

	// A ceremony begun while Turnstile was off cannot finish once it is on.
	unchecked, _ := newTestAPI(t)
	begin := beginPasskeySignup(unchecked, "sneaky", "", remote)
	_, finish := createPasskey(t, begin, nil, func(cookie *http.Cookie, answer []byte) *httptest.ResponseRecorder {
		return postWith(handler, "/api/v1/auth/passkeys/signup/finish", "", cookie, answer)
	})
	expectCode(t, "finish of an unchecked begin", finish, http.StatusForbidden, "turnstile_failed")

	// Provider sign-up: the username step checks its own action.
	callback := steamSignIn(t, handler, "/api/v1/auth/steam/start", "", "76561197960287930")
	signup := cookieNamed(callback, "expo_signup")
	complete := func(token string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"username": "steam_human", "turnstile_token": token})
		return postWith(handler, "/api/v1/auth/signup", "", signup, body)
	}
	expectCode(t, "provider sign-up with the passkey form's token",
		complete("pass:signup-passkey"), http.StatusForbidden, "turnstile_failed")

	down.Store(true)
	expectCode(t, "provider sign-up while siteverify is down",
		complete("pass:signup-provider"), http.StatusServiceUnavailable, "turnstile_unavailable")
	expectCode(t, "passkey begin while siteverify is down",
		beginPasskeySignup(handler, "later", "pass:signup-passkey", remote), http.StatusServiceUnavailable, "turnstile_unavailable")
	down.Store(false)

	if created := complete("pass:signup-provider"); created.Code != http.StatusOK || sessionToken(created) == "" {
		t.Fatalf("provider sign-up with a good token = %d: %s", created.Code, created.Body)
	}
}

func TestRegistrationBudgetChargedOnlyOnSuccess(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Limiter = ratelimit.New(1000)
	})
	signUp := func(name, remote string) int {
		return passkeySignupFrom(t, handler, name, "", remote).Code
	}

	// Failures cost nothing: invalid names, finishes without a ceremony.
	const v4 = "198.51.100.7:1234"
	for i := range 5 {
		if code := beginPasskeySignup(handler, "x", "", v4).Code; code != http.StatusBadRequest {
			t.Fatalf("invalid username %d = %d, want 400", i, code)
		}
	}
	for i := range 3 {
		if code := signUp(fmt.Sprintf("v4-user-%d", i), v4); code != http.StatusOK {
			t.Fatalf("sign-up %d = %d, want 200", i, code)
		}
	}
	if code := beginPasskeySignup(handler, "v4-user-3", "", v4).Code; code != http.StatusTooManyRequests {
		t.Errorf("fourth sign-up from one address = %d, want 429", code)
	}
	if code := signUp("other-v4", "198.51.100.8:1234"); code != http.StatusOK {
		t.Errorf("sign-up from another IPv4 address = %d, want 200", code)
	}

	// IPv6: three per /64, ten per /48.
	created := 0
	for subnet := 0; created < 10; subnet++ {
		for range 3 {
			if created == 10 {
				break
			}
			remote := fmt.Sprintf("[2001:db8:1:%x::5]:1234", subnet)
			if code := signUp(fmt.Sprintf("v6-user-%d", created), remote); code != http.StatusOK {
				t.Fatalf("IPv6 sign-up %d from %s = %d, want 200", created, remote, code)
			}
			created++
		}
	}
	if code := beginPasskeySignup(handler, "v6-user-10", "", "[2001:db8:1:ff::5]:1234").Code; code != http.StatusTooManyRequests {
		t.Errorf("eleventh sign-up from one /48 = %d, want 429", code)
	}
	if code := signUp("v6-elsewhere", "[2001:db8:2::5]:1234"); code != http.StatusOK {
		t.Errorf("sign-up from another /48 = %d, want 200", code)
	}
}
