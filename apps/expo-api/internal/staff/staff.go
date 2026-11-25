// Package staff identifies staff on the admin listener. Staff are not expo
// accounts: they sign in through an identity-aware proxy in front of the console,
// and expo-admin verifies its JWT and takes the email from it. expo-admin
// calls the admin listener with a shared token (API_ADMIN_TOKEN) and names
// the staff member in UserHeader. The token proves the caller is expo-admin;
// the listener trusts the name it sends. See docs/admin.md.
package staff

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// UserHeader names the staff member a request is made for.
const UserHeader = "X-Staff-User"

// MinTokenLength is the shortest shared token accepted.
const MinTokenLength = 32

// maxUsernameRunes bounds a staff username.
const maxUsernameRunes = 64

// Identity is a staff member. Username is the Access email, the name
// staff actions are recorded under.
type Identity struct {
	Username string `json:"username"`
}

var (
	// ErrBadToken means the bearer token is missing or wrong.
	ErrBadToken = errors.New("missing or wrong staff token")
	// ErrNoUser means the token is right but no valid username was named.
	ErrNoUser = errors.New("missing or invalid staff username")
)

// Authenticator checks the shared token and reads the staff username.
type Authenticator struct {
	// tokenHash is the token's SHA-256, so the comparison is constant time
	// whatever the length of what is sent.
	tokenHash [sha256.Size]byte
}

// NewAuthenticator builds an authenticator for the shared token.
func NewAuthenticator(token string) (*Authenticator, error) {
	if len(token) < MinTokenLength {
		return nil, errors.New("the staff token must be at least 32 characters")
	}
	return &Authenticator{tokenHash: sha256.Sum256([]byte(token))}, nil
}

// Authenticate checks a request's bearer token and returns who it is for.
func (a *Authenticator) Authenticate(r *http.Request) (Identity, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	sent := sha256.Sum256([]byte(raw))
	if !ok || subtle.ConstantTimeCompare(sent[:], a.tokenHash[:]) != 1 {
		return Identity{}, ErrBadToken
	}
	username := strings.TrimSpace(r.Header.Get(UserHeader))
	if !validUsername(username) {
		return Identity{}, ErrNoUser
	}
	return Identity{Username: username}, nil
}

func validUsername(username string) bool {
	if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > maxUsernameRunes {
		return false
	}
	for _, r := range username {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
