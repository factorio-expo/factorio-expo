package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// execTestSQL runs a statement against TEST_DATABASE_URL (to age a session).
func execTestSQL(t *testing.T, query string, args ...any) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// ageSignIns makes every session of the user an hour past its sign-in, so
// sensitive changes need "confirm it's you".
func ageSignIns(t *testing.T, userID string) {
	t.Helper()
	execTestSQL(t, "UPDATE user_sessions SET auth_time = now() - interval '1 hour' WHERE user_id = $1", userID)
}

type listedSession struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`
}

func listSessions(t *testing.T, handler http.Handler, token string) []listedSession {
	t.Helper()
	recorder := serve(handler, authedRequest(http.MethodGet, "/api/v1/me/sessions", token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("list sessions = %d: %s", recorder.Code, recorder.Body)
	}
	var body struct{ Sessions []listedSession }
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Sessions
}

func TestSessionsListAndSignOut(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	device, signUp := signUpWithPasskey(t, handler, "many-devices")
	laptop := sessionToken(signUp)
	phone := sessionToken(signInWithPasskey(t, handler, device))
	tablet := sessionToken(signInWithPasskey(t, handler, device))
	other, _ := registerTestUser(t, handler, "someone-else")

	listed := listSessions(t, handler, laptop)
	if len(listed) != 3 {
		t.Fatalf("sessions = %+v, want three", listed)
	}
	var current []string
	for _, session := range listed {
		if session.CreatedAt == "" || session.LastUsedAt == "" || session.ExpiresAt == "" {
			t.Errorf("session without dates: %+v", session)
		}
		if session.Current {
			current = append(current, session.ID)
		}
	}
	if len(current) != 1 {
		t.Fatalf("current sessions = %v, want exactly one", current)
	}
	me := func(token string) int {
		return serve(handler, authedRequest(http.MethodGet, "/api/v1/me", token, nil)).Code
	}

	// Another account cannot end these sessions; the owner ends one.
	var phoneID string
	for _, session := range listSessions(t, handler, phone) {
		if session.Current {
			phoneID = session.ID
		}
	}
	if code := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/sessions/"+phoneID, other, nil)).Code; code != http.StatusNotFound {
		t.Errorf("ending another account's session = %d, want 404", code)
	}
	if code := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/sessions/not-an-id", laptop, nil)).Code; code != http.StatusNotFound {
		t.Errorf("ending a malformed id = %d, want 404", code)
	}
	if code := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/sessions/"+phoneID, laptop, nil)).Code; code != http.StatusNoContent {
		t.Fatalf("ending the phone's session = %d, want 204", code)
	}
	if me(phone) != http.StatusUnauthorized || me(laptop) != http.StatusOK || me(tablet) != http.StatusOK {
		t.Fatalf("after ending one: %d %d %d", me(phone), me(laptop), me(tablet))
	}

	// Ending this device's own session clears its cookie.
	own := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/sessions/"+current[0], laptop, nil))
	if own.Code != http.StatusNoContent || !clearsCookie(own, "expo_session") || me(laptop) != http.StatusUnauthorized {
		t.Fatalf("ending this device = %d", own.Code)
	}

	// Signing out everywhere ends the rest, and no other account's.
	laptop = sessionToken(signInWithPasskey(t, handler, device))
	everywhere := serve(handler, authedRequest(http.MethodDelete, "/api/v1/me/sessions", laptop, nil))
	if everywhere.Code != http.StatusNoContent || !clearsCookie(everywhere, "expo_session") {
		t.Fatalf("sign out everywhere = %d", everywhere.Code)
	}
	if me(laptop) != http.StatusUnauthorized || me(tablet) != http.StatusUnauthorized || me(other) != http.StatusOK {
		t.Fatalf("after signing out everywhere: %d %d, other %d", me(laptop), me(tablet), me(other))
	}
}
