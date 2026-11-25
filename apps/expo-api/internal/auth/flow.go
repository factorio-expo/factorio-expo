package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidFlow is returned for a flow token that is forged, expired or of
// another kind.
var ErrInvalidFlow = errors.New("invalid or expired sign-in flow")

// FlowSigner signs the short-lived state a sign-in ceremony carries between
// its two requests in a cookie (OAuth state, a pending sign-up, a WebAuthn
// challenge). Its key is derived from the session secret but differs from
// it, so a flow token can never pass as a session token.
type FlowSigner struct {
	keys *keyring
}

type flowClaims struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
	jwt.RegisteredClaims
}

// NewFlowSigner derives the flow key from the session secret, and a second
// verify-only key from previousSecret (may be empty), rotating like the
// session tokens.
func NewFlowSigner(sessionSecret, previousSecret string) *FlowSigner {
	return &FlowSigner{keys: newKeyring(flowKey, sessionSecret, previousSecret)}
}

func flowKey(sessionSecret string) []byte {
	mac := hmac.New(sha256.New, []byte(sessionSecret))
	mac.Write([]byte("factorio-expo sign-in flow v1"))
	return mac.Sum(nil)
}

// Sign seals data as a token of the given kind, valid for ttl.
func (f *FlowSigner) Sign(kind string, data any, ttl time.Duration) (string, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := flowClaims{Kind: kind, Data: raw,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl))}
	return f.keys.sign(claims)
}

// Open verifies a token of the given kind and decodes its data into out.
func (f *FlowSigner) Open(kind, token string, out any) error {
	claims := &flowClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, f.keys.keyfunc, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid || claims.Kind != kind {
		return ErrInvalidFlow
	}
	if err := json.Unmarshal(claims.Data, out); err != nil {
		return ErrInvalidFlow
	}
	return nil
}
