package sessions_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/auth"
	"factorio-expo/apps/expo-api/internal/sessions"
	"factorio-expo/internal/shortid"
)

func TestFresh(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		age  time.Duration
		want bool
	}{{0, true}, {9 * time.Minute, true}, {sessions.FreshSignIn, false}, {time.Hour, false}} {
		if got := (sessions.Session{AuthTime: now.Add(-c.age)}).Fresh(now); got != c.want {
			t.Errorf("signed in %v ago: fresh = %v, want %v", c.age, got, c.want)
		}
	}
}

// newService connects to TEST_DATABASE_URL and creates a fresh account.
func newService(t *testing.T) (*sessions.Service, *pgxpool.Pool, string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return sessions.New(pool, auth.NewTokenIssuer("test-secret", ""), time.Hour), pool, addUser(t, pool)
}

func addUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := shortid.New()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO users (id, username) VALUES ($1, $2)", id, "sessions-"+id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func exec(t *testing.T, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func lastUsed(t *testing.T, pool *pgxpool.Pool, id string) time.Time {
	t.Helper()
	var at time.Time
	if err := pool.QueryRow(context.Background(),
		"SELECT last_used_at FROM user_sessions WHERE id = $1", id).Scan(&at); err != nil {
		t.Fatal(err)
	}
	return at
}

func TestStartAuthenticateAndEnd(t *testing.T) {
	service, pool, userID := newService(t)
	ctx := context.Background()
	token, err := service.Start(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if session.UserID != userID || !session.Fresh(time.Now()) || session.ExpiresAt.Sub(session.CreatedAt) < 59*time.Minute {
		t.Fatalf("session = %+v", session)
	}

	// A forged token and a token of another key never authenticate.
	for _, bad := range []string{"not-a-token", mustIssue(t, auth.NewTokenIssuer("other-secret", ""), session.ID)} {
		if _, err := service.Authenticate(ctx, bad); !errors.Is(err, sessions.ErrNotFound) {
			t.Errorf("Authenticate(%q) = %v, want ErrNotFound", bad, err)
		}
	}

	// Ending it revokes the token; ending it again is ErrNotFound, and
	// another account cannot end it.
	other := addUser(t, pool)
	if err := service.End(ctx, other, session.ID); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("End by another account = %v, want ErrNotFound", err)
	}
	if err := service.End(ctx, userID, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, token); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("ended session = %v, want ErrNotFound", err)
	}
	if err := service.End(ctx, userID, session.ID); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("End twice = %v, want ErrNotFound", err)
	}
}

func mustIssue(t *testing.T, issuer *auth.TokenIssuer, sessionID string) string {
	t.Helper()
	token, err := issuer.Issue(sessionID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAuthenticateRefusesExpiredAndDeleted(t *testing.T) {
	service, pool, userID := newService(t)
	ctx := context.Background()
	token, err := service.Start(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, pool, "UPDATE user_sessions SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 second' WHERE user_id = $1", userID)
	if _, err := service.Authenticate(ctx, token); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("expired session = %v, want ErrNotFound", err)
	}
	if list, err := service.List(ctx, userID); err != nil || len(list) != 0 {
		t.Fatalf("listed expired sessions = %v, %v", list, err)
	}

	token, err = service.Start(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	exec(t, pool, "UPDATE users SET deleted_at = now() WHERE id = $1", userID)
	if _, err := service.Authenticate(ctx, token); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("deleted account's session = %v, want ErrNotFound", err)
	}
}

func TestAuthenticateThrottlesLastUsed(t *testing.T) {
	service, pool, userID := newService(t)
	ctx := context.Background()
	token, err := service.Start(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}

	// Used a minute ago: no write.
	exec(t, pool, "UPDATE user_sessions SET last_used_at = now() - interval '1 minute' WHERE id = $1", session.ID)
	before := lastUsed(t, pool, session.ID)
	if _, err := service.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if after := lastUsed(t, pool, session.ID); !after.Equal(before) {
		t.Errorf("last used moved from %v to %v within the throttle", before, after)
	}

	// Used an hour ago: refreshed.
	exec(t, pool, "UPDATE user_sessions SET last_used_at = now() - interval '1 hour' WHERE id = $1", session.ID)
	if _, err := service.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if after := lastUsed(t, pool, session.ID); time.Since(after) > time.Minute {
		t.Errorf("last used = %v, want refreshed", after)
	}
}

func TestConfirmListEndAllAndPrune(t *testing.T) {
	service, pool, userID := newService(t)
	ctx := context.Background()
	first, err := service.Start(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(ctx, userID); err != nil {
		t.Fatal(err)
	}
	session, err := service.Authenticate(ctx, first)
	if err != nil {
		t.Fatal(err)
	}

	// An old sign-in is stale until confirmed again.
	exec(t, pool, "UPDATE user_sessions SET auth_time = now() - interval '1 hour' WHERE user_id = $1", userID)
	if session, _ = service.Authenticate(ctx, first); session.Fresh(time.Now()) {
		t.Fatal("an hour-old sign-in is fresh")
	}
	if err := service.Confirm(ctx, session); err != nil {
		t.Fatal(err)
	}
	if session, _ = service.Authenticate(ctx, first); !session.Fresh(time.Now()) {
		t.Fatal("a confirmed session is not fresh")
	}
	stranger := sessions.Session{ID: session.ID, UserID: addUser(t, pool)}
	if err := service.Confirm(ctx, stranger); !errors.Is(err, sessions.ErrNotFound) {
		t.Fatalf("Confirm of another account's session = %v, want ErrNotFound", err)
	}

	if list, err := service.List(ctx, userID); err != nil || len(list) != 2 {
		t.Fatalf("sessions = %v, %v; want two", list, err)
	}
	if err := service.EndAll(ctx, userID); err != nil {
		t.Fatal(err)
	}
	if list, err := service.List(ctx, userID); err != nil || len(list) != 0 {
		t.Fatalf("sessions after EndAll = %v, %v", list, err)
	}

	// The sweep removes expired rows only.
	if _, err := service.Start(ctx, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(ctx, userID); err != nil {
		t.Fatal(err)
	}
	exec(t, pool, `UPDATE user_sessions SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 second'
		WHERE id = (SELECT id FROM user_sessions WHERE user_id = $1 LIMIT 1)`, userID)
	if pruned, err := service.PruneExpired(ctx); err != nil || pruned < 1 {
		t.Fatalf("PruneExpired = %d, %v", pruned, err)
	}
	var left int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM user_sessions WHERE user_id = $1", userID).Scan(&left); err != nil || left != 1 {
		t.Fatalf("sessions left = %d, %v; want the live one", left, err)
	}
}
