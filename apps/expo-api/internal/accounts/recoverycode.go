package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/fault"
)

// A recovery code lets the person who lost every sign-in method back in: it
// is 128 random bits, shown once when the account is created, and only its
// SHA-256 is stored (users.recovery_code_hash). That much entropy makes a
// slow hash pointless and online guessing hopeless, so the username plus the
// code is as strong as a password. A recovery uses the code up and issues a
// new one in the same step.

// ErrRecoveryFailed: the username and code do not match a live account that
// has a code. One error for every cause, so it tells nothing about which
// usernames exist.
var ErrRecoveryFailed = fault.New(fault.Unauthorized, "recovery_failed", "that username and recovery code do not match")

const (
	recoveryCodeBytes = 16
	// recoveryCodeLength is the code's characters without separators:
	// ceil(128 / 5).
	recoveryCodeLength = 26
	// recoveryGroup is the characters between the dashes shown to people.
	recoveryGroup = 5
)

// recoveryEncoding is base32 without 0, 1, I and O, which are easy to
// misread when the code is copied by hand.
var recoveryEncoding = base32.NewEncoding("ABCDEFGHJKLMNPQRSTUVWXYZ23456789").WithPadding(base32.NoPadding)

// newRecoveryCode makes a fresh code, as people see it (upper case, in
// groups) with the hash to store.
func newRecoveryCode() (string, []byte, error) {
	raw := make([]byte, recoveryCodeBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate recovery code: %w", err)
	}
	return formatRecoveryCode(recoveryEncoding.EncodeToString(raw)), hashRecoveryBytes(raw), nil
}

// formatRecoveryCode groups the code's characters with dashes.
func formatRecoveryCode(plain string) string {
	var groups []string
	for len(plain) > recoveryGroup {
		groups = append(groups, plain[:recoveryGroup])
		plain = plain[recoveryGroup:]
	}
	return strings.Join(append(groups, plain), "-")
}

func hashRecoveryBytes(raw []byte) []byte {
	sum := sha256.Sum256(append([]byte("expo-recovery-code-v1:"), raw...))
	return sum[:]
}

// parseRecoveryCode returns the hash of a code as typed: dashes, whitespace
// and case do not matter. False for anything that is not a code this service
// could have issued.
func parseRecoveryCode(typed string) ([]byte, bool) {
	plain := strings.ToUpper(strings.ReplaceAll(strings.Join(strings.Fields(typed), ""), "-", ""))
	if len(plain) != recoveryCodeLength {
		return nil, false
	}
	raw, err := recoveryEncoding.DecodeString(plain)
	// Re-encoding rejects the trailing bits a decoder ignores, so each code
	// has exactly one spelling.
	if err != nil || recoveryEncoding.EncodeToString(raw) != plain {
		return nil, false
	}
	return hashRecoveryBytes(raw), true
}

// RegenerateRecoveryCode replaces the account's recovery code: the old code
// stops working at once. It returns the new code, shown once.
func (s *Service) RegenerateRecoveryCode(ctx context.Context, userID string) (string, error) {
	code, hash, err := newRecoveryCode()
	if err != nil {
		return "", err
	}
	changed, err := s.queries.ReplaceRecoveryCodeHash(ctx, db.ReplaceRecoveryCodeHashParams{ID: userID, RecoveryCodeHash: hash})
	if err != nil {
		return "", fmt.Errorf("replace recovery code: %w", err)
	}
	if changed == 0 {
		return "", ErrNotFound
	}
	return code, nil
}

// Recover signs in the account of a username and recovery code, using the
// code up: the new one is returned for the person to keep.
func (s *Service) Recover(ctx context.Context, username, code string) (User, string, error) {
	hash, valid := parseRecoveryCode(code)
	account, err := s.queries.GetRecoveryAccount(ctx, username)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return User{}, "", ErrRecoveryFailed
	case err != nil:
		return User{}, "", fmt.Errorf("find recovery account: %w", err)
	}
	if !valid || len(account.RecoveryCodeHash) == 0 ||
		subtle.ConstantTimeCompare(hash, account.RecoveryCodeHash) != 1 {
		return User{}, "", ErrRecoveryFailed
	}
	fresh, freshHash, err := newRecoveryCode()
	if err != nil {
		return User{}, "", err
	}
	used, err := s.queries.RotateRecoveryCodeHash(ctx, db.RotateRecoveryCodeHashParams{
		ID: account.ID, UsedHash: hash, NewHash: freshHash,
	})
	if err != nil {
		return User{}, "", fmt.Errorf("use recovery code: %w", err)
	}
	if used == 0 {
		// Another request used the code between the check and here.
		return User{}, "", ErrRecoveryFailed
	}
	return User{ID: account.ID, Username: account.Username}, fresh, nil
}
