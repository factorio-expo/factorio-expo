package accounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/internal/shortid"
)

// FindByIdentity returns the account a provider identity signs in to, and
// records the sign-in. ErrNotFound means a new person (offer sign-up); a
// deleted account's identities are gone, so it offers sign-up too, unless
// the account was banned: ErrIdentityHeld while its ban lasts.
func (s *Service) FindByIdentity(ctx context.Context, provider, providerUserID string) (User, error) {
	row, err := s.queries.GetIdentityUser(ctx, db.GetIdentityUserParams{
		Provider: provider, ProviderUserID: providerUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.identityHeld(ctx, s.queries, provider, providerUserID); err != nil {
			return User{}, err
		}
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find identity: %w", err)
	}
	if err := s.queries.TouchIdentity(ctx, db.TouchIdentityParams{
		Provider: provider, ProviderUserID: providerUserID,
	}); err != nil {
		return User{}, fmt.Errorf("touch identity: %w", err)
	}
	return User{ID: row.ID, Username: row.Username}, nil
}

// CreateWithIdentity creates an account whose first sign-in method is a
// provider identity (not one a ban holds). It returns the account's recovery
// code.
func (s *Service) CreateWithIdentity(ctx context.Context, username, provider, providerUserID string) (User, string, error) {
	var user User
	var code string
	err := s.inTx(ctx, func(q *db.Queries) error {
		if err := s.identityHeld(ctx, q, provider, providerUserID); err != nil {
			return err
		}
		var err error
		if user, code, err = s.createUser(ctx, q, shortid.New(), username); err != nil {
			return err
		}
		return insertIdentity(ctx, q, user.ID, provider, providerUserID)
	})
	return user, code, err
}

// LinkIdentity adds a provider identity to a signed-in account. Linking one
// that is already this account's is a no-op; one a ban holds is refused.
func (s *Service) LinkIdentity(ctx context.Context, userID, provider, providerUserID string) error {
	owner, err := s.queries.GetIdentityUser(ctx, db.GetIdentityUserParams{
		Provider: provider, ProviderUserID: providerUserID,
	})
	if err == nil {
		if owner.ID == userID {
			return nil
		}
		return ErrIdentityTaken
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("find identity: %w", err)
	}
	return s.inTx(ctx, func(q *db.Queries) error {
		if _, err := lockMethods(ctx, q, userID); err != nil {
			return err
		}
		if err := s.identityHeld(ctx, q, provider, providerUserID); err != nil {
			return err
		}
		return insertIdentity(ctx, q, userID, provider, providerUserID)
	})
}

func insertIdentity(ctx context.Context, q *db.Queries, userID, provider, providerUserID string) error {
	err := q.CreateIdentity(ctx, db.CreateIdentityParams{
		Provider: provider, ProviderUserID: providerUserID, UserID: userID,
	})
	switch {
	case err == nil:
		return nil
	case constraintViolated(err, "auth_identities_pkey"):
		return ErrIdentityTaken
	case constraintViolated(err, "auth_identities_user_id_provider_key"):
		return ErrProviderLinked
	default:
		return fmt.Errorf("link identity: %w", err)
	}
}

// RemoveIdentity unlinks a provider from the account, unless it is the
// account's last sign-in method, and ends every session but keepSession.
func (s *Service) RemoveIdentity(ctx context.Context, userID, provider, keepSession string) error {
	return s.removeMethod(ctx, userID, keepSession, func(q *db.Queries) (int64, error) {
		removed, err := q.DeleteUserIdentity(ctx, db.DeleteUserIdentityParams{UserID: userID, Provider: provider})
		if err != nil {
			return 0, fmt.Errorf("remove identity: %w", err)
		}
		return removed, nil
	})
}
