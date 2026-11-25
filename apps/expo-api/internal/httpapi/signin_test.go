package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"

	"factorio-expo/apps/expo-api/internal/identity"
	"factorio-expo/apps/expo-api/internal/passkey"
)

// testSite is the site origin the test API trusts; passkeys are bound to it.
const testSite = "http://localhost:3000"

var testRP = virtualwebauthn.RelyingParty{ID: "localhost", Name: "Factorio Expo", Origin: testSite}

// testIdentities enables the providers env names (none for nil), calling
// them through client.
func testIdentities(t *testing.T, env map[string]string, client ...*http.Client) *identity.Registry {
	t.Helper()
	httpClient := http.DefaultClient
	if len(client) > 0 {
		httpClient = client[0]
	}
	registry, err := identity.Load("http://localhost:8080", func(name string) string { return env[name] }, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func testPasskeys(t *testing.T) *passkey.Service {
	t.Helper()
	service, err := passkey.New(testSite)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// testDevice is a virtual authenticator holding one passkey.
type testDevice struct {
	authenticator virtualwebauthn.Authenticator
	key           virtualwebauthn.Credential
}

// cookieNamed returns the cookie a response set under name (plain or
// __Host- prefixed), or nil.
func cookieNamed(recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if (cookie.Name == name || cookie.Name == "__Host-"+name) && cookie.MaxAge > 0 {
			return cookie
		}
	}
	return nil
}

// postWith posts a JSON body carrying a cookie and an optional bearer token.
func postWith(handler http.Handler, path, token string, cookie *http.Cookie, body []byte) *httptest.ResponseRecorder {
	request := authedRequest(http.MethodPost, path, token, body)
	if cookie != nil {
		request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	return serve(handler, request)
}

// createPasskey runs a begin/finish registration ceremony on a fresh virtual
// authenticator. begin is the begin response; finish posts the answer.
func createPasskey(t *testing.T, begin *httptest.ResponseRecorder, userHandle []byte,
	finish func(cookie *http.Cookie, answer []byte) *httptest.ResponseRecorder,
) (testDevice, *httptest.ResponseRecorder) {
	t.Helper()
	if begin.Code != http.StatusOK {
		t.Fatalf("begin = %d: %s", begin.Code, begin.Body)
	}
	options, err := virtualwebauthn.ParseAttestationOptions(begin.Body.String())
	if err != nil {
		t.Fatalf("parse creation options: %v", err)
	}
	if userHandle == nil {
		userHandle = []byte(options.UserID)
	}
	device := testDevice{
		authenticator: virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
			UserHandle: userHandle,
		}),
		key: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2),
	}
	answer := virtualwebauthn.CreateAttestationResponse(testRP, device.authenticator, device.key, *options)
	device.authenticator.AddCredential(device.key)
	return device, finish(cookieNamed(begin, "expo_passkey"), []byte(answer))
}

// signUpWithPasskey creates an account through the passkey sign-up
// ceremony and returns the finish response and the device.
func signUpWithPasskey(t *testing.T, handler http.Handler, username string) (testDevice, *httptest.ResponseRecorder) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username})
	begin := postWith(handler, "/api/v1/auth/passkeys/signup/begin", "", nil, body)
	return createPasskey(t, begin, nil, func(cookie *http.Cookie, answer []byte) *httptest.ResponseRecorder {
		return postWith(handler, "/api/v1/auth/passkeys/signup/finish", "", cookie, answer)
	})
}

// signInWithPasskey runs the passkey sign-in ceremony with device.
func signInWithPasskey(t *testing.T, handler http.Handler, device testDevice) *httptest.ResponseRecorder {
	t.Helper()
	begin := postWith(handler, "/api/v1/auth/passkeys/login/begin", "", nil, nil)
	if begin.Code != http.StatusOK {
		t.Fatalf("login begin = %d: %s", begin.Code, begin.Body)
	}
	options, err := virtualwebauthn.ParseAssertionOptions(begin.Body.String())
	if err != nil {
		t.Fatalf("parse request options: %v", err)
	}
	answer := virtualwebauthn.CreateAssertionResponse(testRP, device.authenticator, device.key, *options)
	return postWith(handler, "/api/v1/auth/passkeys/login/finish", "", cookieNamed(begin, "expo_passkey"), []byte(answer))
}

// registerTestUser creates an account through the real passkey sign-up and
// returns its session token (used as a bearer token) and user id.
func registerTestUser(t *testing.T, handler http.Handler, username string) (token, userID string) {
	t.Helper()
	_, finish := signUpWithPasskey(t, handler, username)
	if finish.Code != http.StatusOK {
		t.Fatalf("passkey sign-up = %d: %s", finish.Code, finish.Body)
	}
	var body struct {
		Token  string `json:"token"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(finish.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode sign-up response: %v", err)
	}
	if body.Token != "" {
		t.Fatal("the session token must never appear in a response body")
	}
	token = sessionToken(finish)
	if token == "" {
		t.Fatal("sign-up did not set a session cookie")
	}
	return token, body.UserID
}

func signInMethods(t *testing.T, handler http.Handler, token string) (providers, passkeys []string) {
	t.Helper()
	recorder := serve(handler, authedRequest(http.MethodGet, "/api/v1/me/sign-in-methods", token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("sign-in methods = %d: %s", recorder.Code, recorder.Body)
	}
	var body struct {
		Identities []struct{ Provider string }
		Passkeys   []struct{ ID string }
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, identity := range body.Identities {
		providers = append(providers, identity.Provider)
	}
	for _, p := range body.Passkeys {
		passkeys = append(passkeys, p.ID)
	}
	return providers, passkeys
}

func TestPasskeySignUpSignInAndMethods(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	device, finish := signUpWithPasskey(t, handler, "passkey-alice")
	token := sessionToken(finish)
	if finish.Code != http.StatusOK || token == "" {
		t.Fatalf("sign-up = %d: %s", finish.Code, finish.Body)
	}

	// The same device signs in again, with no username.
	signIn := signInWithPasskey(t, handler, device)
	if signIn.Code != http.StatusOK || !strings.Contains(signIn.Body.String(), `"username":"passkey-alice"`) {
		t.Fatalf("passkey sign-in = %d: %s", signIn.Code, signIn.Body)
	}
	if code := serve(handler, authedRequest(http.MethodGet, "/api/v1/me", sessionToken(signIn), nil)).Code; code != http.StatusOK {
		t.Fatalf("/api/v1/me after passkey sign-in = %d", code)
	}

	// The only method cannot be removed.
	_, passkeys := signInMethods(t, handler, token)
	if len(passkeys) != 1 {
		t.Fatalf("passkeys = %v, want one", passkeys)
	}
	remove := func(id string) int {
		return serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/passkeys/"+id, token, nil)).Code
	}
	if code := remove(passkeys[0]); code != http.StatusConflict {
		t.Fatalf("removing the last method = %d, want 409", code)
	}

	// Add a second passkey from the signed-in session, then remove the first.
	begin := postWith(handler, "/api/v1/me/passkeys/begin", token, nil, nil)
	second, added := createPasskey(t, begin, nil, func(cookie *http.Cookie, answer []byte) *httptest.ResponseRecorder {
		return postWith(handler, "/api/v1/me/passkeys/finish", token, cookie, answer)
	})
	if added.Code != http.StatusNoContent {
		t.Fatalf("add passkey = %d: %s", added.Code, added.Body)
	}
	if _, passkeys = signInMethods(t, handler, token); len(passkeys) != 2 {
		t.Fatalf("passkeys after adding = %v, want two", passkeys)
	}
	if code := remove(passkeys[0]); code != http.StatusNoContent {
		t.Fatalf("remove first passkey = %d, want 204", code)
	}
	if code := signInWithPasskey(t, handler, device).Code; code != http.StatusUnauthorized {
		t.Errorf("removed passkey sign-in = %d, want 401", code)
	}
	if code := signInWithPasskey(t, handler, second).Code; code != http.StatusOK {
		t.Errorf("second passkey sign-in = %d, want 200", code)
	}
}

func TestPasskeySignUpRules(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	begin := func(username string) int {
		body, _ := json.Marshal(map[string]string{"username": username})
		return postWith(handler, "/api/v1/auth/passkeys/signup/begin", "", nil, body).Code
	}
	for _, username := range []string{"<script>", "../admin", strings.Repeat("a", 33), "ab", "has@at"} {
		if code := begin(username); code != http.StatusBadRequest {
			t.Errorf("username %q = %d, want 400", username, code)
		}
	}
	registerTestUser(t, handler, "alice")
	if code := begin("ALICE"); code != http.StatusConflict {
		t.Errorf("case variant of a taken username = %d, want 409", code)
	}
	// Reserved names are unavailable in any case and with '_' or '-' mixed
	// in; a longer name that contains one is fine.
	for _, username := range []string{"admin", "Ad_min", "A-D-M-I-N", "privacy_policy", "Factorio-Expo"} {
		if code := begin(username); code != http.StatusConflict {
			t.Errorf("reserved username %q = %d, want 409", username, code)
		}
	}
	if code := begin("admin-alice"); code != http.StatusOK {
		t.Errorf("a name containing a reserved one = %d, want 200", code)
	}

	// A sign-up ceremony's cookie and answer work once.
	body, _ := json.Marshal(map[string]string{"username": "replay-me"})
	started := postWith(handler, "/api/v1/auth/passkeys/signup/begin", "", nil, body)
	var saved *http.Cookie
	var savedAnswer []byte
	_, first := createPasskey(t, started, nil, func(cookie *http.Cookie, answer []byte) *httptest.ResponseRecorder {
		saved, savedAnswer = cookie, answer
		return postWith(handler, "/api/v1/auth/passkeys/signup/finish", "", cookie, answer)
	})
	if first.Code != http.StatusOK {
		t.Fatalf("sign-up = %d: %s", first.Code, first.Body)
	}
	if code := postWith(handler, "/api/v1/auth/passkeys/signup/finish", "", saved, savedAnswer).Code; code == http.StatusOK {
		t.Error("a replayed sign-up succeeded")
	}

	// A sign-in challenge cannot finish a sign-up.
	login := postWith(handler, "/api/v1/auth/passkeys/login/begin", "", nil, nil)
	cookie := cookieNamed(login, "expo_passkey")
	if code := postWith(handler, "/api/v1/auth/passkeys/signup/finish", "", cookie, []byte(`{"id":"x","rawId":"x","type":"public-key","response":{"clientDataJSON":"x","attestationObject":"x"}}`)).Code; code != http.StatusBadRequest {
		t.Errorf("sign-up finished with a sign-in cookie = %d, want 400", code)
	}
}

func TestNoProvidersConfigured(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	recorder := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/providers", "", nil))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"providers":[]`)) {
		t.Fatalf("providers = %d: %s", recorder.Code, recorder.Body)
	}
	if code := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/github/start", "", nil)).Code; code != http.StatusNotFound {
		t.Errorf("start of a disabled provider = %d, want 404", code)
	}
	callback := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/github/callback?state=x&code=y", "", nil))
	if callback.Code != http.StatusFound || callback.Header().Get("Location") != testSite+"/login?error=expired" {
		t.Errorf("callback without a flow = %d to %q", callback.Code, callback.Header().Get("Location"))
	}
	if code := serve(handler, authedRequest(http.MethodGet, "/api/v1/auth/signup", "", nil)).Code; code != http.StatusNotFound {
		t.Errorf("pending sign-up without a flow = %d, want 404", code)
	}
}
