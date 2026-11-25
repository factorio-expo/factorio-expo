package auth

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/golang-jwt/jwt/v5"
)

// keyring signs with the current secret and verifies with the current and
// the previous one, so API_JWT_SECRET can be rotated without signing
// everyone out. Every token names its key in the `kid` header; a token
// naming an unknown key (or none) is refused.
type keyring struct {
	currentID string
	keys      map[string][]byte
}

// newKeyring derives each secret's signing key with derive. previous may be
// empty.
func newKeyring(derive func(secret string) []byte, current, previous string) *keyring {
	r := &keyring{currentID: keyID(current), keys: map[string][]byte{}}
	r.keys[r.currentID] = derive(current)
	if previous != "" {
		r.keys[keyID(previous)] = derive(previous)
	}
	return r
}

// keyID names a secret without revealing it.
func keyID(secret string) string {
	sum := sha256.Sum256([]byte("factorio-expo key id v1\x00" + secret))
	return hex.EncodeToString(sum[:4])
}

// sign seals claims with the current key.
func (r *keyring) sign(claims jwt.Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = r.currentID
	return token.SignedString(r.keys[r.currentID])
}

// keyfunc picks the verification key by the token's `kid` header.
func (r *keyring) keyfunc(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	key, ok := r.keys[kid]
	if !ok {
		return nil, ErrInvalidToken
	}
	return key, nil
}
