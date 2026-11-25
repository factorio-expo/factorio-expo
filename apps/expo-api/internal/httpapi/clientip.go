package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIPConfig says which peers may vouch for the real client address.
// Production runs behind a reverse proxy that resolves the visitor's address
// and overwrites Header; expo-web forwards it on SSR calls. A request from any
// other peer is keyed by its own socket address, so the header cannot be
// spoofed by clients that reach the API directly.
type ClientIPConfig struct {
	TrustedProxies []netip.Prefix
	Header         string
}

// ParseTrustedProxies parses a comma-separated CIDR list.
func ParseTrustedProxies(value string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for part := range strings.SplitSeq(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", part, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// clientIP returns the caller's address for rate limiting.
func (c ClientIPConfig) clientIP(r *http.Request) netip.Addr {
	peer := remoteAddr(r)
	if c.Header == "" || !c.trusted(peer) {
		return peer
	}
	forwarded := strings.TrimSpace(r.Header.Get(c.Header))
	// Tolerate an X-Forwarded-For style list: the left-most entry is the
	// client as resolved by the trusted edge.
	if first, _, found := strings.Cut(forwarded, ","); found {
		forwarded = strings.TrimSpace(first)
	}
	addr, err := netip.ParseAddr(forwarded)
	if err != nil {
		return peer
	}
	return addr.Unmap()
}

func (c ClientIPConfig) trusted(addr netip.Addr) bool {
	for _, prefix := range c.TrustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.IPv6Unspecified()
	}
	return addr.Unmap()
}

// clientKey is the rate-limit key for an address. IPv6 clients are keyed by
// their /64, since a single host usually controls a whole /64.
func clientKey(addr netip.Addr) string {
	if addr.Is6() {
		prefix, _ := addr.Prefix(64)
		return prefix.String()
	}
	return addr.String()
}
