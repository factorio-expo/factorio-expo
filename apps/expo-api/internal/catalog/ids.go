package catalog

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/internal/shortid"
)

// parseID checks a public identifier, reporting malformed input as
// ErrNotFound because it can never name an existing row.
func parseID(value string) (string, error) {
	if !shortid.Valid(value) {
		return "", ErrNotFound
	}
	return value, nil
}

// newEditToken generates the unguessable secret that lets an anonymous
// uploader edit or delete their entry later. It is shown once, at upload time.
func newEditToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashEditToken is what entries store instead of the edit token (hex
// SHA-256), so a database dump or backup grants no edit rights. The token has
// 256 random bits, so a plain hash needs no salt or stretching. Empty stays
// empty (no token).
func hashEditToken(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

// subtleEqual compares secrets in constant time so edit tokens cannot be
// recovered by timing repeated requests.
func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// pgBool is a nullable boolean: nil leaves a coalesced column as it is.
func pgBool(value *bool) pgtype.Bool {
	if value == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *value, Valid: true}
}

// optionalTime is a nullable timestamp as a pointer (nil when null).
func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}
