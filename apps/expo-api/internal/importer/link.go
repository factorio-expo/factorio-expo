package importer

import (
	"net/netip"
	"net/url"
	"regexp"
	"strings"
)

// maxLinkLength bounds what is parsed at all.
const maxLinkLength = 2048

// parsedLink is a pasted link that passed the structural checks: https, no
// credentials, no port, a DNS-name host, lower-cased. Only its host and path
// segments are used from here on; the raw text never reaches a request.
type parsedLink struct {
	host     string
	segments []string
}

// parseLink applies the checks every host shares. It does not decide whether
// the host is supported.
func parseLink(raw string) (parsedLink, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxLinkLength {
		return parsedLink{}, ErrInvalidLink
	}
	for _, r := range raw {
		// Control characters, spaces and backslashes are how parsers disagree.
		if r <= ' ' || r == 0x7f || r == '\\' {
			return parsedLink{}, ErrInvalidLink
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Host == "" {
		return parsedLink{}, ErrInvalidLink
	}
	// u.Port() is empty for "host" and "host:"; a colon in the authority at
	// all (a port, even :443, or an IPv6 literal) is refused.
	if strings.ContainsAny(u.Host, ":[]@") {
		return parsedLink{}, ErrInvalidLink
	}
	host := strings.ToLower(u.Hostname())
	if _, err := netip.ParseAddr(host); err == nil || !dnsName.MatchString(host) {
		return parsedLink{}, ErrInvalidLink
	}
	var segments []string
	for segment := range strings.SplitSeq(strings.Trim(u.Path, "/"), "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return parsedLink{host: host, segments: segments}, nil
}

// dnsName is a plain ASCII host name: labels of letters, digits and hyphens,
// with at least one dot and no trailing dot (so also no IPv4 literal: the
// last label must start with a letter).
var dnsName = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
