package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken is returned when a token fails signature verification,
// has expired, or does not carry the expected claims.
var ErrInvalidToken = errors.New("invalid or expired token")

// Claims are the session token's claims. The token names a server-side
// session (internal/sessions), which holds the account and when the person
// last signed in; deleting the session revokes the token.
type Claims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

// TokenIssuer signs session tokens with the current secret and verifies
// them with the current or the previous one (key rotation, keyring.go).
type TokenIssuer struct {
	keys *keyring
}

// NewTokenIssuer builds a TokenIssuer signing with secret. previous, when
// not empty, is the retired secret whose tokens still verify.
func NewTokenIssuer(secret, previous string) *TokenIssuer {
	return &TokenIssuer{keys: newKeyring(func(s string) []byte { return []byte(s) }, secret, previous)}
}

// Issue creates a signed JWT for a session, valid until expires.
func (i *TokenIssuer) Issue(sessionID string, expires time.Time) (string, error) {
	claims := Claims{
		SessionID: sessionID,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(expires),
	}
	return i.keys.sign(claims)
}

// Verify parses and validates a signed token, returning its claims.
func (i *TokenIssuer) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, i.keys.keyfunc, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.SessionID == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
