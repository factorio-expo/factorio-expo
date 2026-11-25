package sessions

import (
	"context"
	"fmt"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

// Info is one session as the account page lists it.
type Info struct {
	ID         string    `json:"id" doc:"The session's id."`
	CreatedAt  time.Time `json:"created_at" doc:"When the device signed in."`
	LastUsedAt time.Time `json:"last_used_at" doc:"When the session was last used."`
	ExpiresAt  time.Time `json:"expires_at" doc:"When the session ends."`
}

// List returns the account's live sessions, most recently used first.
func (s *Service) List(ctx context.Context, userID string) ([]Info, error) {
	rows, err := s.queries.ListUserSessions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	list := make([]Info, 0, len(rows))
	for _, row := range rows {
		list = append(list, Info{
			ID: row.ID, CreatedAt: row.CreatedAt.Time, LastUsedAt: row.LastUsedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		})
	}
	return list, nil
}

// End signs one of the account's sessions out. ErrNotFound when the account
// has no such session.
func (s *Service) End(ctx context.Context, userID, sessionID string) error {
	ended, err := s.queries.DeleteUserSession(ctx, db.DeleteUserSessionParams{ID: sessionID, UserID: userID})
	if err != nil {
		return fmt.Errorf("end session: %w", err)
	}
	if ended == 0 {
		return ErrNotFound
	}
	return nil
}

// EndAll signs the account out everywhere.
func (s *Service) EndAll(ctx context.Context, userID string) error {
	if _, err := s.queries.DeleteUserSessions(ctx, userID); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	return nil
}

// Confirm records that the person completed a sign-in ceremony again on
// session ("confirm it's you"), which makes it fresh.
func (s *Service) Confirm(ctx context.Context, session Session) error {
	confirmed, err := s.queries.ConfirmSessionSignIn(ctx, db.ConfirmSessionSignInParams{
		ID: session.ID, UserID: session.UserID,
	})
	if err != nil {
		return fmt.Errorf("confirm session: %w", err)
	}
	if confirmed == 0 {
		return ErrNotFound
	}
	return nil
}

// PruneExpired deletes expired sessions (the API's hourly sweep).
func (s *Service) PruneExpired(ctx context.Context) (int64, error) {
	pruned, err := s.queries.DeleteExpiredSessions(ctx)
	if err != nil {
		return 0, fmt.Errorf("prune sessions: %w", err)
	}
	return pruned, nil
}
