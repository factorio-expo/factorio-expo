package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestClientIPTrustsHeaderOnlyFromTrustedProxies(t *testing.T) {
	trusted, err := ParseTrustedProxies("172.16.0.0/12, ::1/128")
	if err != nil {
		t.Fatal(err)
	}
	config := ClientIPConfig{TrustedProxies: trusted, Header: "X-Real-IP"}
	cases := []struct {
		name, remote, header, want string
	}{
		{"trusted proxy names the client", "172.18.0.5:4000", "203.0.113.9", "203.0.113.9"},
		{"trusted proxy, list form", "172.18.0.5:4000", "203.0.113.9, 10.0.0.1", "203.0.113.9"},
		{"trusted proxy without header", "172.18.0.5:4000", "", "172.18.0.5"},
		{"garbage header falls back to peer", "[::1]:4000", "not-an-ip", "::1"},
		{"untrusted peer cannot spoof", "198.51.100.4:4000", "203.0.113.9", "198.51.100.4"},
	}
	for _, c := range cases {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = c.remote
		if c.header != "" {
			request.Header.Set("X-Real-IP", c.header)
		}
		if got := config.clientIP(request).String(); got != c.want {
			t.Errorf("%s: clientIP = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestClientKeyGroupsIPv6By64(t *testing.T) {
	config := ClientIPConfig{}
	a := httptest.NewRequest("GET", "/", nil)
	a.RemoteAddr = "[2001:db8:1:2::1]:1"
	b := httptest.NewRequest("GET", "/", nil)
	b.RemoteAddr = "[2001:db8:1:2:ffff::9]:1"
	if clientKey(config.clientIP(a)) != clientKey(config.clientIP(b)) {
		t.Fatal("addresses in one /64 must share a rate-limit key")
	}
}
