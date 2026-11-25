package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// recoveryCodeShape is a recovery code as people see it: 26 characters
// (128 bits) in groups of five, without 0, 1, I or O.
var recoveryCodeShape = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{5}(-[A-HJ-NP-Z2-9]{5}){4}-[A-HJ-NP-Z2-9]$`)

// recoveryCodeOf is the recovery code in a sign-up or recovery response ("" when absent).
func recoveryCodeOf(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		RecoveryCode string `json:"recovery_code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %d %s: %v", recorder.Code, recorder.Body, err)
	}
	return body.RecoveryCode
}

func recoverWith(handler http.Handler, username, code string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"username": username, "recovery_code": code})
	return postWith(handler, "/api/v1/auth/recover", "", nil, body)
}

// expectRecoveryFailed checks the one answer every mismatch gets.
func expectRecoveryFailed(t *testing.T, name string, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var body struct{ Code string }
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != http.StatusUnauthorized || body.Code != "recovery_failed" {
		t.Errorf("%s = %d %s, want 401 recovery_failed", name, recorder.Code, recorder.Body)
	}
	if sessionToken(recorder) != "" || recoveryCodeOf(t, recorder) != "" {
		t.Errorf("%s gave out a session or a code: %s", name, recorder.Body)
	}
}

func TestSignUpIssuesARecoveryCodeAndSignInDoesNot(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	device, signUp := signUpWithPasskey(t, handler, "code-alice")
	if signUp.Code != http.StatusOK {
		t.Fatalf("sign-up = %d: %s", signUp.Code, signUp.Body)
	}
	code := recoveryCodeOf(t, signUp)
	if !recoveryCodeShape.MatchString(code) {
		t.Fatalf("sign-up recovery code = %q, want 26 characters in groups of five", code)
	}
	// Only a hash is stored: the code is in no other response.
	if login := signInWithPasskey(t, handler, device); login.Code != http.StatusOK || recoveryCodeOf(t, login) != "" {
		t.Errorf("a passkey sign-in = %d %s, want no recovery code", login.Code, login.Body)
	}
	if me := serve(handler, authedRequest(http.MethodGet, "/api/v1/me", sessionToken(signUp), nil)); strings.Contains(me.Body.String(), "recovery") {
		t.Errorf("/me mentions the recovery code: %s", me.Body)
	}
	// Two accounts never share a code.
	_, other := signUpWithPasskey(t, handler, "code-bob")
	if recoveryCodeOf(t, other) == code {
		t.Fatal("two accounts got the same recovery code")
	}
}

func TestRecoverSignsInUsesTheCodeUpAndIssuesANewOne(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	device, signUp := signUpWithPasskey(t, handler, "lost-alice")
	first := recoveryCodeOf(t, signUp)
	userID := signedUpID(t, signUp)
	_, bobSignUp := signUpWithPasskey(t, handler, "found-bob")
	bobCode, bob := recoveryCodeOf(t, bobSignUp), sessionToken(bobSignUp)

	// Typed sloppily: any case, no dashes, any username case.
	sloppy := strings.ToLower(strings.ReplaceAll(first, "-", ""))
	recovered := recoverWith(handler, "LOST-ALICE", " "+sloppy+" ")
	if recovered.Code != http.StatusOK || !strings.Contains(recovered.Body.String(), `"username":"lost-alice"`) {
		t.Fatalf("recovery = %d: %s", recovered.Code, recovered.Body)
	}
	token, second := sessionToken(recovered), recoveryCodeOf(t, recovered)
	if token == "" || !recoveryCodeShape.MatchString(second) || second == first {
		t.Fatalf("recovery gave session %q and code %q (old %q)", token, second, first)
	}
	if code := serve(handler, authedRequest(http.MethodGet, "/api/v1/me", token, nil)).Code; code != http.StatusOK {
		t.Fatalf("/me with the recovered session = %d", code)
	}

	// The code is used up; the new one works, and so on.
	expectRecoveryFailed(t, "reusing the code", recoverWith(handler, "lost-alice", first))
	third := recoverWith(handler, "lost-alice", second)
	if third.Code != http.StatusOK || recoveryCodeOf(t, third) == second {
		t.Fatalf("recovery with the new code = %d: %s", third.Code, third.Body)
	}
	expectRecoveryFailed(t, "the second code after its use", recoverWith(handler, "lost-alice", second))

	// The session is a fresh sign-in: sign-in methods change at once, which
	// is how a person who lost a login replaces it.
	if begin := postWith(handler, "/api/v1/me/passkeys/begin", token, nil, nil); begin.Code != http.StatusOK {
		t.Fatalf("add a passkey right after recovering = %d: %s", begin.Code, begin.Body)
	}
	// ... and ending the old session is not part of recovery: the other
	// devices stay signed in, the passkey keeps working.
	if login := signInWithPasskey(t, handler, device); login.Code != http.StatusOK {
		t.Errorf("the old passkey after a recovery = %d", login.Code)
	}
	// Alice's recovery did not touch bob's account or code.
	if code := serve(handler, authedRequest(http.MethodGet, "/api/v1/me", bob, nil)).Code; code != http.StatusOK {
		t.Errorf("bob's session after alice's recovery = %d", code)
	}
	if bobs := recoverWith(handler, "found-bob", bobCode); bobs.Code != http.StatusOK {
		t.Errorf("bob's own code = %d: %s", bobs.Code, bobs.Body)
	}
	// Another account's code does not open this one.
	expectRecoveryFailed(t, "bob's old code on alice", recoverWith(handler, "lost-alice", bobCode))
	_ = userID
}

func TestRecoverRefusesEveryMismatchTheSameWay(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	_, signUp := signUpWithPasskey(t, handler, "same-alice")
	code := recoveryCodeOf(t, signUp)
	userID := signedUpID(t, signUp)

	// A well-formed code that is not hers: flip the last character.
	last := code[len(code)-1]
	other := byte('A')
	if last == 'A' {
		other = 'B'
	}
	wrong := code[:len(code)-1] + string(other)
	for name, recorder := range map[string]*httptest.ResponseRecorder{
		"a wrong code":            recoverWith(handler, "same-alice", wrong),
		"an unknown username":     recoverWith(handler, "nobody-here", code),
		"a malformed code":        recoverWith(handler, "same-alice", "not a code"),
		"a code too short":        recoverWith(handler, "same-alice", code[:20]),
		"a code too long":         recoverWith(handler, "same-alice", code+"A"),
		"look-alike characters":   recoverWith(handler, "same-alice", strings.ReplaceAll(code, "A", "0")+"0"),
		"the username as a code":  recoverWith(handler, "same-alice", "same-alice"),
		"another user's username": recoverWith(handler, "same-alicf", code),
	} {
		expectRecoveryFailed(t, name, recorder)
	}
	// Refusals use nothing up.
	if ok := recoverWith(handler, "same-alice", code); ok.Code != http.StatusOK {
		t.Fatalf("the code after refusals = %d: %s", ok.Code, ok.Body)
	}

	// An account without a code (none issued) cannot be recovered.
	execTestSQL(t, "UPDATE users SET recovery_code_hash = NULL WHERE id = $1", userID)
	expectRecoveryFailed(t, "an account without a code", recoverWith(handler, "same-alice", code))

	// Missing fields are a plain bad request, not a guess.
	for name, body := range map[string]string{
		"no username": `{"recovery_code":"` + code + `"}`,
		"no code":     `{"username":"same-alice"}`,
		"empty":       `{}`,
	} {
		if got := postWith(handler, "/api/v1/auth/recover", "", nil, []byte(body)); got.Code != http.StatusBadRequest && got.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d, want 400", name, got.Code)
		}
	}
}

func TestRecoverUsedByTwoRequestsWorksOnce(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	_, signUp := signUpWithPasskey(t, handler, "race-alice")
	code := recoveryCodeOf(t, signUp)

	const attempts = 8
	results := make([]*httptest.ResponseRecorder, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Go(func() {
			results[i] = recoverWith(handler, "race-alice", code)
		})
	}
	wg.Wait()
	succeeded := 0
	for _, recorder := range results {
		switch recorder.Code {
		case http.StatusOK:
			succeeded++
		case http.StatusUnauthorized:
		default:
			t.Errorf("concurrent recovery = %d: %s", recorder.Code, recorder.Body)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of %d concurrent recoveries with one code succeeded, want exactly 1", succeeded, attempts)
	}
}

func TestRecoverRefusesADeletedAccount(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	_, signUp := signUpWithPasskey(t, handler, "gone-alice")
	code, token, userID := recoveryCodeOf(t, signUp), sessionToken(signUp), signedUpID(t, signUp)

	if deleted := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me", token, nil)); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete account = %d: %s", deleted.Code, deleted.Body)
	}
	expectRecoveryFailed(t, "a deleted account", recoverWith(handler, "gone-alice", code))
	// The hash goes with the account's other sign-in methods.
	execTestSQL(t, "DO $$ BEGIN IF EXISTS (SELECT 1 FROM users WHERE id = '"+userID+"' AND recovery_code_hash IS NOT NULL) THEN RAISE EXCEPTION 'recovery code hash kept'; END IF; END $$")
}

func TestNewRecoveryCodeReplacesTheOldOne(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	device, signUp := signUpWithPasskey(t, handler, "regen-alice")
	first, token, userID := recoveryCodeOf(t, signUp), sessionToken(signUp), signedUpID(t, signUp)
	// A signed-out caller gets nothing.
	if code := postWith(handler, "/api/v1/me/recovery-code", "", nil, nil).Code; code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", code)
	}

	issued := postWith(handler, "/api/v1/me/recovery-code", token, nil, nil)
	second := recoveryCodeOf(t, issued)
	if issued.Code != http.StatusOK || !recoveryCodeShape.MatchString(second) || second == first {
		t.Fatalf("new recovery code = %d %s (old %q)", issued.Code, issued.Body, first)
	}
	expectRecoveryFailed(t, "the replaced code", recoverWith(handler, "regen-alice", first))
	used := recoverWith(handler, "regen-alice", second)
	if used.Code != http.StatusOK {
		t.Fatalf("the new code = %d: %s", used.Code, used.Body)
	}
	current := recoveryCodeOf(t, used) // the recovery replaced it again
	// Other sessions are untouched.
	if login := signInWithPasskey(t, handler, device); login.Code != http.StatusOK {
		t.Errorf("passkey sign-in after regenerating = %d", login.Code)
	}

	// Like every sensitive change it needs a recent sign-in, and a refusal
	// replaces nothing.
	ageSignIns(t, userID)
	expectReauthRequired(t, "new recovery code", postWith(handler, "/api/v1/me/recovery-code", token, nil, nil))
	if again := recoverWith(handler, "regen-alice", current); again.Code != http.StatusOK {
		t.Errorf("a refused regeneration changed the code: %d %s", again.Code, again.Body)
	}
}
