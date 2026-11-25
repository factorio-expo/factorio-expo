package importer

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicAddress(t *testing.T) {
	for address, want := range map[string]bool{
		"93.184.216.34":          true,
		"140.82.112.3":           true,
		"2606:4700:4700::1111":   true,
		"127.0.0.1":              false,
		"127.255.255.254":        false,
		"::1":                    false,
		"10.0.0.1":               false,
		"172.16.5.4":             false,
		"172.31.255.255":         false,
		"192.168.1.1":            false,
		"169.254.169.254":        false, // cloud metadata
		"169.254.0.1":            false,
		"fe80::1":                false,
		"fc00::1":                false,
		"fd12:3456:789a::1":      false,
		"fd00:ec2::254":          false, // AWS IPv6 metadata
		"0.0.0.0":                false,
		"::":                     false,
		"100.64.0.1":             false, // carrier-grade NAT
		"100.127.255.255":        false,
		"192.0.0.8":              false,
		"192.0.2.1":              false,
		"198.18.0.1":             false,
		"198.51.100.7":           false,
		"203.0.113.9":            false,
		"224.0.0.1":              false,
		"239.255.255.250":        false,
		"ff02::1":                false,
		"240.0.0.1":              false,
		"255.255.255.255":        false,
		"::ffff:127.0.0.1":       false, // IPv4-mapped
		"::ffff:10.0.0.1":        false,
		"::ffff:169.254.169.254": false,
		"::ffff:93.184.216.34":   true,
		"64:ff9b::7f00:1":        false, // NAT64
		"2002:7f00:1::1":         false, // 6to4
		"2001:db8::1":            false,
		"fec0::1":                false,
		"::7f00:1":               false, // IPv4-compatible 127.0.0.1
		"::a9fe:a9fe":            false, // IPv4-compatible 169.254.169.254
		"::ffff:0:7f00:1":        false, // SIIT-translated 127.0.0.1
		"3fff::1":                false, // documentation (RFC 9637)
		"5f00::1":                false, // SRv6 SIDs
		"2000::1":                true,  // inside 2000::/3
		"3ffe::1":                true,
		"4000::1":                false, // outside 2000::/3
		"8000::1":                false,
	} {
		if got := publicAddress(netip.MustParseAddr(address)); got != want {
			t.Errorf("publicAddress(%s) = %v, want %v", address, got, want)
		}
	}
}

func TestGuardAddress(t *testing.T) {
	for address, ok := range map[string]bool{
		"93.184.216.34:443":          true,
		"[2606:4700:4700::1111]:443": true,
		"93.184.216.34:80":           false,
		"93.184.216.34:8443":         false,
		"127.0.0.1:443":              false,
		"[::1]:443":                  false,
		"10.1.2.3:443":               false,
		"169.254.169.254:443":        false,
		"not an address":             false,
	} {
		network := "tcp4"
		if strings.HasPrefix(address, "[") {
			network = "tcp6"
		}
		err := guardAddress(network, address, nil)
		if (err == nil) != ok {
			t.Errorf("guardAddress(%s) = %v, want ok=%v", address, err, ok)
		}
		if err != nil && !errors.Is(err, errBlockedAddress) {
			t.Errorf("guardAddress(%s) = %v, not a blocked-address error", address, err)
		}
	}
	if err := guardAddress("unix", "/tmp/sock", nil); err == nil {
		t.Error("a unix socket was allowed")
	}
}

// The production transport refuses to connect to a loopback server: the
// request never arrives, whatever name or address the link resolves to.
func TestProductionTransportRefusesPrivateServers(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		text(exchange)(w, nil)
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())

	for name, base := range map[string]string{
		"address": server.URL,
		"name":    "http://localhost:" + port,
	} {
		t.Run(name, func(t *testing.T) {
			fetcher, err := New(Config{
				Sites: RebaseSites(DefaultSites(), base), MaxExchangeBytes: 1 << 20, Timeout: 2 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fetcher.Fetch(context.Background(), "https://pastebin.com/aBcD1234"); !errors.Is(err, ErrFetchFailed) {
				t.Fatalf("err = %v, want a failed fetch", err)
			}
			if requests.Load() != 0 {
				t.Fatal("the server was reached")
			}
		})
	}
}

// A name that resolves to a private address is refused at connect time, after
// the resolution: the dialer's Control hook sees the literal address.
func TestDialerChecksTheResolvedAddress(t *testing.T) {
	transport := newTransport(guardAddress)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, address := range []string{"localhost:443", "127.0.0.1:443", "[::1]:443"} {
		conn, err := transport.DialContext(ctx, "tcp", address)
		if err == nil {
			conn.Close()
			t.Fatalf("connected to %s", address)
		}
		if !errors.Is(err, errBlockedAddress) {
			t.Logf("%s: %v", address, err)
		}
	}
}

func TestTransportIgnoresTheEnvironmentProxy(t *testing.T) {
	if newTransport(guardAddress).Proxy != nil {
		t.Fatal("the import transport has a proxy")
	}
}
