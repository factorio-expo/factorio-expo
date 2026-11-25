package auth

import (
	"errors"
	"fmt"
	"strings"
)

// MinSecretBytes is the shortest accepted signing or hashing key.
const MinSecretBytes = 32

// ValidateSecret rejects a server key (API_JWT_SECRET, API_IP_HASH_SECRET)
// that is short or still the .env.example placeholder. Placeholders are
// public, and user ids are too, so anyone could sign a session for any
// account with them.
func ValidateSecret(secret string) error {
	if len(secret) < MinSecretBytes {
		return fmt.Errorf("must be at least %d bytes", MinSecretBytes)
	}
	if strings.Contains(strings.ToLower(secret), "change-me") {
		return errors.New("is still the .env.example placeholder; generate one with `openssl rand -base64 48`")
	}
	return nil
}
