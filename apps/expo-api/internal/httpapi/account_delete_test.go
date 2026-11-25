package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// signedUpID is the account id in a sign-up response.
func signedUpID(t *testing.T, signUp *httptest.ResponseRecorder) string {
	t.Helper()
	var me struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(signUp.Body.Bytes(), &me); err != nil || me.UserID == "" {
		t.Fatalf("sign-up response %d %s: %v", signUp.Code, signUp.Body, err)
	}
	return me.UserID
}

// sessionCleared reports whether the response expires the session cookie.
func sessionCleared(recorder *httptest.ResponseRecorder) bool {
	for _, cookie := range recorder.Result().Cookies() {
		if (cookie.Name == "expo_session" || cookie.Name == "__Host-expo_session") && cookie.MaxAge < 0 {
			return true
		}
	}
	return false
}

func usernameRefused(t *testing.T, handler http.Handler, username string) bool {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username})
	return postWith(handler, "/api/v1/auth/passkeys/signup/begin", "", nil, body).Code == http.StatusConflict
}

func TestDeleteOwnAccount(t *testing.T) {
	truncateSocialFixtures(t)
	handler := newSteamTestAPI(t)
	const aliceSteam = "76561197960287950"
	device, signUp := signUpWithPasskey(t, handler, "leaving-alice")
	token, aliceID := sessionToken(signUp), signedUpID(t, signUp)
	if linked := steamSignIn(t, handler, "/api/v1/auth/steam/start?link=1", token, aliceSteam); location(linked) != testSite+"/account?linked=steam" {
		t.Fatalf("linking went to %q", location(linked))
	}
	phone := sessionToken(signInWithPasskey(t, handler, device))
	bob, _ := registerTestUser(t, handler, "staying-bob")
	me := func(token string) int {
		return serve(handler, authedRequest(http.MethodGet, "/api/v1/me", token, nil)).Code
	}
	deleteMe := func(token string) *httptest.ResponseRecorder {
		return serve(handler, authedRequest(http.MethodDelete, "/api/v1/me", token, nil))
	}

	// Deleting needs a recent sign-in, like every sensitive change.
	ageSignIns(t, aliceID)
	expectReauthRequired(t, "delete account", deleteMe(token))
	if me(token) != http.StatusOK {
		t.Fatal("a refused deletion signed the account out")
	}
	if confirmed := confirmWithPasskey(t, handler, token, token, device); confirmed.Code != http.StatusNoContent {
		t.Fatalf("confirm with a passkey = %d: %s", confirmed.Code, confirmed.Body)
	}
	deleted := deleteMe(token)
	if deleted.Code != http.StatusNoContent || !sessionCleared(deleted) {
		t.Fatalf("delete account = %d (cookie cleared: %v): %s", deleted.Code, sessionCleared(deleted), deleted.Body)
	}

	// Every token of the account is dead at once: this device's, another
	// device's, and the same token sent again. Another account is untouched.
	if me(token) != http.StatusUnauthorized || me(phone) != http.StatusUnauthorized || me(bob) != http.StatusOK {
		t.Errorf("after deletion: this device %d, other device %d, other account %d; want 401, 401, 200",
			me(token), me(phone), me(bob))
	}
	if code := deleteMe(token).Code; code != http.StatusUnauthorized {
		t.Errorf("deleting again with the old token = %d, want 401", code)
	}
	if code := serve(handler, authedRequest(http.MethodGet, "/api/v1/me/sessions", phone, nil)).Code; code != http.StatusUnauthorized {
		t.Errorf("old token on the sessions list = %d, want 401", code)
	}
	if code := serve(handler, authedRequest(http.MethodGet, "/api/v1/users/leaving-alice", "", nil)).Code; code != http.StatusNotFound {
		t.Errorf("profile after deletion = %d, want 404", code)
	}

	// Nobody can sign in to it again: its passkey is gone, and its Steam
	// account is a stranger now (offered a new sign-up, not this account).
	if login := signInWithPasskey(t, handler, device); login.Code != http.StatusUnauthorized || sessionToken(login) != "" {
		t.Errorf("passkey sign-in after deletion = %d, want 401 without a session", login.Code)
	}
	steam := steamSignIn(t, handler, "/api/v1/auth/steam/start", "", aliceSteam)
	if location(steam) != testSite+"/signup" || sessionToken(steam) != "" {
		t.Errorf("Steam sign-in after deletion went to %q", location(steam))
	}
	// The username stays held by the row until the hard delete.
	if !usernameRefused(t, handler, "leaving-alice") {
		t.Error("the deleted account's username can be taken at once")
	}
}

func TestDeleteBannedAccountHoldsTheBan(t *testing.T) {
	truncateSocialFixtures(t)
	handler := newSteamTestAPI(t)
	const bobSteam = "76561197960287960"
	token, bobID := registerTestUser(t, handler, "banned-bob")
	if linked := steamSignIn(t, handler, "/api/v1/auth/steam/start?link=1", token, bobSteam); location(linked) != testSite+"/account?linked=steam" {
		t.Fatalf("linking went to %q", location(linked))
	}
	execTestSQL(t, `INSERT INTO user_bans (id, user_id, actions, reason_code, banned_by, expires_at)
		VALUES ('banTestBob01', $1, ARRAY['comment'], 'spam', 'staff', now() + interval '3 days')`, bobID)
	if code := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me", token, nil)).Code; code != http.StatusNoContent {
		t.Fatalf("delete banned account = %d", code)
	}
	// The daily sweep has hard-deleted the row (and the ban with it).
	execTestSQL(t, "DELETE FROM users WHERE id = $1", bobID)

	// While the ban would have lasted, its Steam account can neither sign up
	// nor be linked to another account, and its username (in any spelling
	// that normalizes alike) stays refused.
	if refused := steamSignIn(t, handler, "/api/v1/auth/steam/start", "", bobSteam); location(refused) != testSite+"/login?error=identity-held" {
		t.Errorf("held Steam sign-in went to %q", location(refused))
	}
	carol, _ := registerTestUser(t, handler, "other-carol")
	if refused := steamSignIn(t, handler, "/api/v1/auth/steam/start?link=1", carol, bobSteam); location(refused) != testSite+"/account?error=identity-held" {
		t.Errorf("linking the held Steam account went to %q", location(refused))
	}
	for _, name := range []string{"banned-bob", "Banned_Bob", "bannedbob"} {
		if !usernameRefused(t, handler, name) {
			t.Errorf("held username %q can be taken", name)
		}
	}
	if usernameRefused(t, handler, "banned-bobby") {
		t.Error("a different username is refused")
	}

	// Once the ban's end passes, both are free again.
	execTestSQL(t, "UPDATE ban_holds SET expires_at = now() - interval '1 second'")
	if steam := steamSignIn(t, handler, "/api/v1/auth/steam/start", "", bobSteam); location(steam) != testSite+"/signup" {
		t.Errorf("Steam sign-in after the hold went to %q", location(steam))
	}
	if usernameRefused(t, handler, "banned-bob") {
		t.Error("the username is still refused after the hold ended")
	}
}
