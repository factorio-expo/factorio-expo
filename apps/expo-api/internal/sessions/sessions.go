// Package sessions keeps signed-in devices: one user_sessions row per
// session token. The token (a JWT, auth.TokenIssuer) names its row, and
// every authenticated request looks the row up, so deleting a row signs that
// device out at once. A row also holds when the person last completed a
// sign-in ceremony (auth_time), which sensitive account changes check
// (Session.Fresh). Rows hold dates only: no IP address, no user agent.
package sessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/auth"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

const (
	// FreshSignIn is how recently the person must have completed a sign-in
	// ceremony on a session for a sensitive account change: linking or
	// removing a sign-in method, adding a passkey, the data export and
	// account deletion.
	FreshSignIn = 10 * time.Minute
	// touchEvery throttles last_used_at: a request refreshes it only when
	// it is older than this, so most requests read the row and write nothing.
	touchEvery = 5 * time.Minute
)

// ErrNotFound: no such live session (unknown, expired, revoked, another
// account's, or its account was deleted), or the token does not verify.
var ErrNotFound = errors.New("session not found")

// Session is one signed-in device.
type Session struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
	// AuthTime is when the person last completed a sign-in ceremony on
	// this session: the sign-in itself, or a later "confirm it's you".
	AuthTime time.Time
}

// Fresh reports whether the person completed a sign-in ceremony on this
// session within FreshSignIn of now.
func (s Session) Fresh(now time.Time) bool {
	return now.Sub(s.AuthTime) < FreshSignIn
}

// Service starts, checks and ends sessions.
type Service struct {
	queries *db.Queries
	tokens  *auth.TokenIssuer
	ttl     time.Duration
}

// New builds the service on the catalog pool; sessions last ttl.
func New(pool *pgxpool.Pool, tokens *auth.TokenIssuer, ttl time.Duration) *Service {
	return &Service{queries: db.New(pool), tokens: tokens, ttl: ttl}
}

// TTL is how long a session lasts from its sign-in.
func (s *Service) TTL() time.Duration { return s.ttl }

// Start creates a session for an account that just signed in and returns
// its token.
func (s *Service) Start(ctx context.Context, userID string) (string, error) {
	id := shortid.New()
	expires := time.Now().Add(s.ttl)
	if err := s.queries.CreateSession(ctx, db.CreateSessionParams{
		ID: id, UserID: userID, ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return s.tokens.Issue(id, expires)
}

// Authenticate verifies a session token and loads its live session.
// ErrNotFound when either fails; other errors are the database's.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	claims, err := s.tokens.Verify(token)
	if err != nil || !shortid.Valid(claims.SessionID) {
		return Session{}, ErrNotFound
	}
	row, err := s.queries.GetLiveSession(ctx, claims.SessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("load session: %w", err)
	}
	session := Session{
		ID: row.ID, UserID: row.UserID, CreatedAt: row.CreatedAt.Time, LastUsedAt: row.LastUsedAt.Time,
		ExpiresAt: row.ExpiresAt.Time, AuthTime: row.AuthTime.Time,
	}
	if time.Since(session.LastUsedAt) >= touchEvery {
		// "Last used" is informational: a failed write never fails the request.
		if err := s.queries.TouchSession(ctx, session.ID); err != nil {
			slog.Warn("touch session", "error", err)
		}
	}
	return session, nil
}
