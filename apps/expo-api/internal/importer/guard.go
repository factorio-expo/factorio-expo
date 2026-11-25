package importer

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// errBlockedAddress is a connection to an address no import may reach.
var errBlockedAddress = errors.New("import: address is not allowed")

// globalUnicast6 is the only IPv6 space allowed: the 2000::/3 global unicast
// block. Everything else (the IPv4-compatible ::/96 and its mapped and
// translated forms, unique-local, link-local, reserved space) is refused.
var globalUnicast6 = netip.MustParsePrefix("2000::/3")

// blockedPrefixes are address ranges outside what netip's own predicates
// (loopback, private, link-local, multicast, unspecified) already refuse:
// carrier-grade NAT, benchmarking and documentation ranges, reserved space,
// and the IPv6 forms that embed or tunnel an IPv4 address, plus the ranges
// inside 2000::/3 that are not for public unicast.
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("::ffff:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
}

// publicAddress reports whether addr is an ordinary public unicast address.
func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	if addr.Is6() && !globalUnicast6.Contains(addr) {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// guardAddress is the dialer's Control hook. It runs once the name has been
// resolved, with the literal address about to be connected to, so a name that
// answers a public address to a check and a private one to the connection
// (DNS rebinding) is refused all the same. Only the https port is allowed.
func guardAddress(network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" {
		return fmt.Errorf("%w: network %s", errBlockedAddress, network)
	}
	target, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %v", errBlockedAddress, err)
	}
	if target.Port() != 443 {
		return fmt.Errorf("%w: port %d", errBlockedAddress, target.Port())
	}
	if !publicAddress(target.Addr()) {
		return fmt.Errorf("%w: %s", errBlockedAddress, target.Addr())
	}
	return nil
}

// newTransport is the one transport imports use: no proxy from the
// environment, a connect-time address check (control; guardAddress in
// production), short timeouts, no connection reuse and no credentials.
func newTransport(control func(network, address string, c syscall.RawConn) error) *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: control}
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            dialer.DialContext,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  8 * time.Second,
		MaxResponseHeaderBytes: 32 << 10,
		DisableKeepAlives:      true,
		DisableCompression:     false,
	}
}
