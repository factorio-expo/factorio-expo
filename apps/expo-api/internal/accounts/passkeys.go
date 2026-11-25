package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/passkey"
	"factorio-expo/internal/shortid"
)

// ErrPasskeyTaken: the credential is already registered (to any account).
var ErrPasskeyTaken = fault.New(fault.Conflict, "passkey_taken", "this passkey is already registered")

// PasskeyUser loads a live account with its passkeys, for a WebAuthn
// ceremony. ErrNotFound for an unknown or deleted account.
func (s *Service) PasskeyUser(ctx context.Context, userID string) (passkey.User, error) {
	user, err := s.queries.GetUserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return passkey.User{}, ErrNotFound
	}
	if err != nil {
		return passkey.User{}, fmt.Errorf("load user: %w", err)
	}
	rows, err := s.queries.ListUserPasskeys(ctx, userID)
	if err != nil {
		return passkey.User{}, fmt.Errorf("load passkeys: %w", err)
	}
	result := passkey.User{ID: user.ID, Username: user.Username}
	for _, row := range rows {
		var credential webauthn.Credential
		if err := json.Unmarshal(row.Credential, &credential); err != nil {
			return passkey.User{}, fmt.Errorf("decode passkey %s: %w", row.ID, err)
		}
		result.Credentials = append(result.Credentials, credential)
	}
	return result, nil
}

// CreateWithPasskey creates an account (id chosen by the caller: it is the
// passkey's user handle) whose first sign-in method is a passkey. It returns
// the account's recovery code.
func (s *Service) CreateWithPasskey(ctx context.Context, userID, username string, credential *webauthn.Credential) (User, string, error) {
	var user User
	var code string
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		if user, code, err = s.createUser(ctx, q, userID, username); err != nil {
			return err
		}
		return insertPasskey(ctx, q, userID, credential)
	})
	return user, code, err
}

// AddPasskey adds a passkey to a signed-in account.
func (s *Service) AddPasskey(ctx context.Context, userID string, credential *webauthn.Credential) error {
	return s.inTx(ctx, func(q *db.Queries) error {
		if _, err := lockMethods(ctx, q, userID); err != nil {
			return err
		}
		return insertPasskey(ctx, q, userID, credential)
	})
}

func insertPasskey(ctx context.Context, q *db.Queries, userID string, credential *webauthn.Credential) error {
	data, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode passkey: %w", err)
	}
	err = q.CreatePasskey(ctx, db.CreatePasskeyParams{
		ID: shortid.New(), UserID: userID, CredentialID: credential.ID, Credential: data,
	})
	if constraintViolated(err, "user_passkeys_credential_id_key") {
		return ErrPasskeyTaken
	}
	if err != nil {
		return fmt.Errorf("store passkey: %w", err)
	}
	return nil
}

// PasskeySignedIn stores a passkey's state after a sign-in (sign count,
// flags) and records when it was used.
func (s *Service) PasskeySignedIn(ctx context.Context, userID string, credential *webauthn.Credential) error {
	data, err := json.Marshal(credential)
	if err != nil {
		return fmt.Errorf("encode passkey: %w", err)
	}
	return s.queries.UpdatePasskeyAfterSignIn(ctx, db.UpdatePasskeyAfterSignInParams{
		UserID: userID, CredentialID: credential.ID, Credential: data,
	})
}

// RemovePasskey deletes one of the account's passkeys, unless it is the
// account's last sign-in method, and ends every session but keepSession.
func (s *Service) RemovePasskey(ctx context.Context, userID, passkeyID, keepSession string) error {
	return s.removeMethod(ctx, userID, keepSession, func(q *db.Queries) (int64, error) {
		removed, err := q.DeleteUserPasskey(ctx, db.DeleteUserPasskeyParams{UserID: userID, ID: passkeyID})
		if err != nil {
			return 0, fmt.Errorf("remove passkey: %w", err)
		}
		return removed, nil
	})
}

// Methods are an account's sign-in methods, for its settings page.
type Methods struct {
	Identities []IdentityMethod `json:"identities"`
	Passkeys   []PasskeyMethod  `json:"passkeys"`
}

// IdentityMethod is one linked provider.
type IdentityMethod struct {
	Provider   string    `json:"provider" doc:"The provider's name."`
	CreatedAt  time.Time `json:"created_at" doc:"When the provider was linked."`
	LastUsedAt time.Time `json:"last_used_at" doc:"When it last signed in."`
}

// PasskeyMethod is one passkey.
type PasskeyMethod struct {
	ID         string     `json:"id" doc:"The passkey's id."`
	CreatedAt  time.Time  `json:"created_at" doc:"When the passkey was added."`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" doc:"When it last signed in."`
}

// ListMethods returns the account's sign-in methods.
func (s *Service) ListMethods(ctx context.Context, userID string) (Methods, error) {
	identities, err := s.queries.ListUserIdentities(ctx, userID)
	if err != nil {
		return Methods{}, fmt.Errorf("list identities: %w", err)
	}
	passkeys, err := s.queries.ListUserPasskeys(ctx, userID)
	if err != nil {
		return Methods{}, fmt.Errorf("list passkeys: %w", err)
	}
	methods := Methods{Identities: []IdentityMethod{}, Passkeys: []PasskeyMethod{}}
	for _, row := range identities {
		methods.Identities = append(methods.Identities, IdentityMethod{
			Provider: row.Provider, CreatedAt: row.CreatedAt.Time, LastUsedAt: row.LastUsedAt.Time,
		})
	}
	for _, row := range passkeys {
		method := PasskeyMethod{ID: row.ID, CreatedAt: row.CreatedAt.Time}
		if row.LastUsedAt.Valid {
			used := row.LastUsedAt.Time
			method.LastUsedAt = &used
		}
		methods.Passkeys = append(methods.Passkeys, method)
	}
	return methods, nil
}
