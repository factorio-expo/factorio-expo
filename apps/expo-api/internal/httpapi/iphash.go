package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"net/http"
)

// ipHash is the keyed hash (HMAC-SHA256 under API_IP_HASH_SECRET) of the
// client's address, IPv6 by its /64 as the rate limits key it; nil when the
// address is unknown. Address bans store only this, never the address.
func (s *server) ipHash(r *http.Request) []byte {
	addr := s.clientIPs.clientIP(r)
	if !addr.IsValid() || addr.IsUnspecified() {
		return nil
	}
	mac := hmac.New(sha256.New, s.ipHashKey)
	mac.Write(visitorAddr(addr).AsSlice())
	return mac.Sum(nil)
}
