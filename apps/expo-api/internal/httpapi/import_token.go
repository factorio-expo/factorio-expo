package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"time"
)

// Imported-source tokens prove where an upload's string came from. Resolving a
// link fetches the blueprint and signs (canonical source link, SHA-256 of the
// exchange string) with an expiry; an upload that names a source_url must
// present that token, and the API checks it against the string actually
// uploaded and the link claimed. Without it anyone could stamp any supported
// site's link on any string.

const (
	// importTokenLifetime is how long a resolve's token stays good: time to
	// review the blueprint and fill in the details.
	importTokenLifetime = 30 * time.Minute
	// importTokenKeyLabel keeps the derived key apart from every other use of
	// the server secret it comes from.
	importTokenKeyLabel = "factorio-expo/import-source-token/v1"
	importTokenMACBytes = sha256.Size
)

// importTokens signs and verifies imported-source tokens.
type importTokens struct {
	key []byte
	now func() time.Time
}

// newImportTokens derives the signing key from secret (HMAC-SHA256 over a
// label), so the secret itself is never used as this key.
func newImportTokens(secret []byte) *importTokens {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(importTokenKeyLabel))
	return &importTokens{key: mac.Sum(nil), now: time.Now}
}

// mac covers the expiry, the canonical source link and the string's hash.
func (t *importTokens) mac(expires int64, sourceURL, exchange string) []byte {
	digest := sha256.Sum256([]byte(exchange))
	mac := hmac.New(sha256.New, t.key)
	var stamp [8]byte
	binary.BigEndian.PutUint64(stamp[:], uint64(expires))
	mac.Write(stamp[:])
	mac.Write([]byte(sourceURL))
	mac.Write([]byte{0})
	mac.Write(digest[:])
	return mac.Sum(nil)
}

// sign returns the token for an exchange string fetched from sourceURL.
func (t *importTokens) sign(sourceURL, exchange string) string {
	expires := t.now().Add(importTokenLifetime).Unix()
	raw := make([]byte, 8, 8+importTokenMACBytes)
	binary.BigEndian.PutUint64(raw, uint64(expires))
	raw = append(raw, t.mac(expires, sourceURL, exchange)...)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// valid reports whether token was signed for exactly this source and string
// and has not expired. The comparison is constant-time.
func (t *importTokens) valid(token, sourceURL, exchange string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 8+importTokenMACBytes {
		return false
	}
	expires := int64(binary.BigEndian.Uint64(raw[:8]))
	if !hmac.Equal(raw[8:], t.mac(expires, sourceURL, exchange)) {
		return false
	}
	return t.now().Unix() < expires
}
