// Package banhold hashes what a deleted banned account leaves behind
// (ban_holds, docs/admin.md#bans-across-account-deletion): its username and
// its provider identities, so a ban outlives the account without the name or
// the provider user id being kept. The catalog writes the hashes when it
// deletes a banned account; accounts refuses sign-ups and links that match.
package banhold

import (
	"crypto/hmac"
	"crypto/sha256"
	"strings"
)

// Kinds of hold (ban_holds.kind).
const (
	KindUsername = "username"
	KindIdentity = "identity"
)

// Hasher makes the keyed hashes of ban holds.
type Hasher struct{ key []byte }

// New derives the hold key from secret (API_IP_HASH_SECRET, which keys the
// address hashes of IP bans too), so a hold hash never equals an address
// hash.
func New(secret string) *Hasher {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("factorio-expo ban holds v1"))
	return &Hasher{key: mac.Sum(nil)}
}

// Username hashes a username normalized like reserved_usernames (lowercase,
// '_' and '-' dropped), so "Bad_Guy" also holds "badguy".
func (h *Hasher) Username(username string) []byte {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(username))
	return h.sum("username", normalized)
}

// Identity hashes a provider identity: provider and the provider's own user
// id.
func (h *Hasher) Identity(provider, providerUserID string) []byte {
	return h.sum("identity", provider, providerUserID)
}

// sum hashes the parts with a zero byte after each, so no two part lists
// hash alike.
func (h *Hasher) sum(parts ...string) []byte {
	mac := hmac.New(sha256.New, h.key)
	for _, part := range parts {
		mac.Write([]byte(part))
		mac.Write([]byte{0})
	}
	return mac.Sum(nil)
}
