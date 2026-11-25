package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/descope/virtualwebauthn"

	"factorio-expo/apps/expo-api/internal/httpapi"
)

// newSteamTestAPI is the test API with Steam sign-in against fakeSteam.
func newSteamTestAPI(t *testing.T) http.Handler {
	t.Helper()
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Identities = testIdentities(t, map[string]string{"API_STEAM_API_KEY": "web-api-key"}, fakeSteam(t))
	})
	return handler
}

// expectReauthRequired checks a refused sensitive change.
func expectReauthRequired(t *testing.T, name string, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var body struct{ Code string }
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != http.StatusForbidden || body.Code != "reauth_required" {
		t.Errorf("%s = %d %s, want 403 reauth_required", name, recorder.Code, recorder.Body)
	}
}

// confirmWithPasskey runs "confirm it's you" for token's account with
// device and returns the finish response.
func confirmWithPasskey(t *testing.T, handler http.Handler, token, finishToken string, device testDevice) *httptest.ResponseRecorder {
	t.Helper()
	begin := postWith(handler, "/api/v1/me/reauth/passkey/begin", token, nil, nil)
	if begin.Code != http.StatusOK {
		t.Fatalf("reauth begin = %d: %s", begin.Code, begin.Body)
	}
	options, err := virtualwebauthn.ParseAssertionOptions(begin.Body.String())
	if err != nil {
		t.Fatalf("parse request options: %v", err)
	}
	answer := virtualwebauthn.CreateAssertionResponse(testRP, device.authenticator, device.key, *options)
	return postWith(handler, "/api/v1/me/reauth/passkey/finish", finishToken, cookieNamed(begin, "expo_passkey"), []byte(answer))
}

func TestSensitiveChangesNeedFreshSignIn(t *testing.T) {
	truncateSocialFixtures(t)
	handler := newSteamTestAPI(t)
	device, signUp := signUpWithPasskey(t, handler, "fresh-alice")
	token := sessionToken(signUp)
	var me struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(signUp.Body.Bytes(), &me); err != nil || me.UserID == "" {
		t.Fatalf("sign-up response %s: %v", signUp.Body, err)
	}

	// Just signed up: fresh, so a second passkey can be added.
	begin := postWith(handler, "/api/v1/me/passkeys/begin", token, nil, nil)
	_, added := createPasskey(t, begin, nil, func(cookie *http.Cookie, answer []byte) *httptest.ResponseRecorder {
		return postWith(handler, "/api/v1/me/passkeys/finish", token, cookie, answer)
	})
	if added.Code != http.StatusNoContent {
		t.Fatalf("add passkey while fresh = %d: %s", added.Code, added.Body)
	}
	phone := sessionToken(signInWithPasskey(t, handler, device))
	_, passkeys := signInMethods(t, handler, token)

	// An hour later every sign-in method change asks to confirm it's you.
	ageSignIns(t, me.UserID)
	expectReauthRequired(t, "remove passkey",
		serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/passkeys/"+passkeys[1], token, nil)))
	expectReauthRequired(t, "remove identity",
		serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/identities/steam", token, nil)))
	expectReauthRequired(t, "add passkey", postWith(handler, "/api/v1/me/passkeys/begin", token, nil, nil))
	link := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/steam/start?link=1", token, nil))
	if link.Code != http.StatusFound || link.Header().Get("Location") != testSite+"/account?error=reauth_required" {
		t.Errorf("stale link = %d to %q", link.Code, link.Header().Get("Location"))
	}

	// Another account's passkey, or a check started by another account,
	// does not confirm alice.
	bobDevice, bobSignUp := signUpWithPasskey(t, handler, "fresh-bob")
	bob := sessionToken(bobSignUp)
	if code := confirmWithPasskey(t, handler, token, token, bobDevice).Code; code != http.StatusForbidden {
		t.Errorf("confirming with another account's passkey = %d, want 403", code)
	}
	if code := confirmWithPasskey(t, handler, token, bob, device).Code; code != http.StatusConflict {
		t.Errorf("finishing another account's check = %d, want 409", code)
	}
	expectReauthRequired(t, "remove passkey after refused checks",
		serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/passkeys/"+passkeys[1], token, nil)))

	// Her own passkey does; the removal then ends every other session.
	if confirmed := confirmWithPasskey(t, handler, token, token, device); confirmed.Code != http.StatusNoContent {
		t.Fatalf("confirm with a passkey = %d: %s", confirmed.Code, confirmed.Body)
	}
	if code := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/passkeys/"+passkeys[1], token, nil)).Code; code != http.StatusNoContent {
		t.Fatalf("remove passkey after confirming = %d, want 204", code)
	}
	meCode := func(token string) int {
		return serve(handler, authedRequest(http.MethodGet, "/api/v1/me", token, nil)).Code
	}
	if meCode(token) != http.StatusOK || meCode(phone) != http.StatusUnauthorized || meCode(bob) != http.StatusOK {
		t.Errorf("after removing a method: this %d, other device %d, other account %d", meCode(token), meCode(phone), meCode(bob))
	}
}

func TestConfirmWithProvider(t *testing.T) {
	truncateSocialFixtures(t)
	handler := newSteamTestAPI(t)
	bob, bobID := registerTestUser(t, handler, "steam-confirm")
	carol, _ := registerTestUser(t, handler, "steam-carol")
	const bobSteam, carolSteam, strangerSteam = "76561197960287940", "76561197960287941", "76561197960287942"
	for token, steamID := range map[string]string{bob: bobSteam, carol: carolSteam} {
		linked := steamSignIn(t, handler, "/api/v1/auth/steam/start?link=1", token, steamID)
		if linked.Header().Get("Location") != testSite+"/account?linked=steam" {
			t.Fatalf("linking went to %q", linked.Header().Get("Location"))
		}
	}
	ageSignIns(t, bobID)

	// A Steam account that is not bob's is refused, and bob stays stale.
	for _, steamID := range []string{carolSteam, strangerSteam} {
		refused := steamSignIn(t, handler, "/api/v1/auth/steam/start?reauth=1", bob, steamID)
		if location(refused) != testSite+"/account?error=reauth-other-account" {
			t.Errorf("confirming as %s went to %q", steamID, location(refused))
		}
	}
	expectReauthRequired(t, "remove identity",
		serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/identities/steam", bob, nil)))

	// Bob's own Steam account confirms him.
	confirmed := steamSignIn(t, handler, "/api/v1/auth/steam/start?reauth=1", bob, bobSteam)
	if location(confirmed) != testSite+"/account?reauth=done" || sessionToken(confirmed) != "" {
		t.Fatalf("confirming went to %q", location(confirmed))
	}
	if code := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/identities/steam", bob, nil)).Code; code != http.StatusNoContent {
		t.Fatalf("remove identity after confirming = %d, want 204", code)
	}

	// Confirming needs a signed-in session.
	signedOut := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/steam/start?reauth=1", "", nil))
	if location(signedOut) != testSite+"/login?next=%2Faccount" {
		t.Errorf("signed-out confirm went to %q", location(signedOut))
	}
}
