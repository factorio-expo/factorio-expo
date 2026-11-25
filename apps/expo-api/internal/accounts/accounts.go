// Package accounts creates accounts and manages their sign-in methods:
// provider identities (auth_identities) and passkeys (user_passkeys). An
// account always keeps at least one method. A deleted banned account's
// username and identities stay refused while its ban holds them (ban_holds,
// internal/banhold). Every account gets a recovery code at its creation
// (recoverycode.go).
package accounts

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/banhold"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
)

var (
	// ErrNotFound: no such account, identity or passkey.
	ErrNotFound = fault.New(fault.NotFound, "not_found", "not found")
	// ErrUsernameTaken: another account has the username (any case), or it
	// is reserved. Both read the same to the person signing up.
	ErrUsernameTaken = fault.New(fault.Conflict, "username_taken", "that username is not available")
	// ErrIdentityTaken: the provider account is linked to another account.
	ErrIdentityTaken = fault.New(fault.Conflict, "identity_taken", "this sign-in is linked to another account")
	// ErrProviderLinked: the account already has another account of this provider.
	ErrProviderLinked = fault.New(fault.Conflict, "provider_linked", "an account of this provider is already linked")
	// ErrLastMethod: removing the method would leave no way to sign in.
	ErrLastMethod = fault.New(fault.Conflict, "last_method", "an account needs at least one sign-in method")
	// ErrIdentityHeld: the provider account belonged to a deleted account
	// whose ban has not ended (ban_holds).
	ErrIdentityHeld = fault.New(fault.Forbidden, "identity_held",
		"this sign-in belonged to a deleted account that is banned; it can be used again once the ban ends")
)

// User is a signed-in account: what a session is started for.
type User struct {
	ID       string
	Username string
}

// Service owns account creation and sign-in methods.
type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	holds   *banhold.Hasher
}

// New builds the service on the catalog pool; holds hashes what ban holds
// are checked against (the catalog's hasher).
func New(pool *pgxpool.Pool, holds *banhold.Hasher) *Service {
	return &Service{pool: pool, queries: db.New(pool), holds: holds}
}

// UsernameAvailable reports whether no account (deleted ones included) holds
// the username in any case, it is not reserved (reserved_usernames: matched
// ignoring case, '_' and '-') and no ban holds it.
func (s *Service) UsernameAvailable(ctx context.Context, username string) (bool, error) {
	unavailable, err := s.usernameUnavailable(ctx, s.queries, username)
	return !unavailable, err
}

func (s *Service) usernameUnavailable(ctx context.Context, q *db.Queries, username string) (bool, error) {
	unavailable, err := q.UsernameUnavailable(ctx, db.UsernameUnavailableParams{
		Username: username, HoldHash: s.holds.Username(username),
	})
	if err != nil {
		return false, fmt.Errorf("check username: %w", err)
	}
	return unavailable, nil
}

// identityHeld returns ErrIdentityHeld when a ban holds the provider account.
func (s *Service) identityHeld(ctx context.Context, q *db.Queries, provider, providerUserID string) error {
	held, err := q.BanHeld(ctx, s.holds.Identity(provider, providerUserID))
	if err != nil {
		return fmt.Errorf("check ban hold: %w", err)
	}
	if held {
		return ErrIdentityHeld
	}
	return nil
}

// createUser inserts the account row inside tx, unless the username is not
// available (checked again here: the sign-up form checked it a step before),
// and issues its recovery code, returned for the person to keep (shown once).
func (s *Service) createUser(ctx context.Context, q *db.Queries, id, username string) (User, string, error) {
	unavailable, err := s.usernameUnavailable(ctx, q, username)
	if err != nil {
		return User{}, "", err
	}
	if unavailable {
		return User{}, "", ErrUsernameTaken
	}
	row, err := q.CreateUser(ctx, db.CreateUserParams{ID: id, Username: username})
	if err != nil {
		if constraintViolated(err, "") {
			return User{}, "", ErrUsernameTaken
		}
		return User{}, "", fmt.Errorf("create user: %w", err)
	}
	code, hash, err := newRecoveryCode()
	if err != nil {
		return User{}, "", err
	}
	if err := q.SetRecoveryCodeHash(ctx, db.SetRecoveryCodeHashParams{ID: row.ID, RecoveryCodeHash: hash}); err != nil {
		return User{}, "", fmt.Errorf("issue recovery code: %w", err)
	}
	return User{ID: row.ID, Username: row.Username}, code, nil
}

// inTx runs fn in one transaction with queries bound to it.
func (s *Service) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(s.queries.WithTx(tx))
	})
}

// lockMethods locks the account row so concurrent removals cannot leave it
// without a sign-in method.
func lockMethods(ctx context.Context, q *db.Queries, userID string) (int64, error) {
	if _, err := q.LockUser(ctx, userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	return q.CountSignInMethods(ctx, userID)
}

// removeMethod deletes one sign-in method with remove, unless it is the
// account's last, and signs the account out everywhere but keepSession (the
// session that asked), in one transaction: whoever held the removed method
// may still be signed in elsewhere.
func (s *Service) removeMethod(ctx context.Context, userID, keepSession string, remove func(q *db.Queries) (int64, error)) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		methods, err := lockMethods(ctx, q, userID)
		if err != nil {
			return err
		}
		if methods <= 1 {
			return ErrLastMethod
		}
		removed, err := remove(q)
		if err != nil {
			return err
		}
		if removed == 0 {
			return ErrNotFound
		}
		if _, err := q.DeleteOtherUserSessions(ctx, db.DeleteOtherUserSessionsParams{
			UserID: userID, KeepID: keepSession,
		}); err != nil {
			return fmt.Errorf("end other sessions: %w", err)
		}
		return nil
	})
}

// constraintViolated reports a unique violation, of the named constraint
// when name is set.
func constraintViolated(err error, name string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (name == "" || pgErr.ConstraintName == name)
}
