package config

import "testing"

func TestCheckPublicURLs(t *testing.T) {
	cases := []struct {
		name      string
		secure    bool
		api, site string
		wantErr   bool
	}{
		{"production", true, "https://api.example.com", "https://example.com", false},
		{"local compose", true, "http://localhost:8080", "http://localhost:3000", false},
		{"loopback ip and ipv6", true, "http://127.0.0.1:8080", "http://[::1]:3000", false},
		{"insecure cookies over http", false, "http://api.example.com", "http://example.com", false},
		{"api over http", true, "http://api.example.com", "https://example.com", true},
		{"site over http", true, "https://api.example.com", "http://example.com", true},
		{"localhost default left in production", true, "http://localhost:8080", "https://example.com", true},
		{"localhost default left, cookies insecure", false, "http://localhost:8080", "https://example.com", true},
		{"api https loopback with real site", true, "https://localhost", "https://example.com", true},
		{"not a URL", true, "api.example.com", "https://example.com", true},
	}
	for _, c := range cases {
		if err := checkPublicURLs(c.secure, c.api, c.site); (err != nil) != c.wantErr {
			t.Errorf("%s: checkPublicURLs = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestCheckTypesenseKey(t *testing.T) {
	cases := []struct {
		key, origins string
		wantErr      bool
	}{
		{"", "http://localhost:3000", true},
		{devTypesenseKey, "http://localhost:3000,https://example.org", false},
		{devTypesenseKey, "https://example.org", true},
		{"a-random-key", "https://example.org", false},
	}
	for _, c := range cases {
		if err := checkTypesenseKey(c.key, c.origins); (err != nil) != c.wantErr {
			t.Errorf("checkTypesenseKey(%q, %q) = %v, want error %v", c.key, c.origins, err, c.wantErr)
		}
	}
}

func TestCheckImportStub(t *testing.T) {
	const local = "http://localhost:3000"
	for _, tc := range []struct {
		name, stub, origins, publicURL string
		secure                         bool
		ok                             bool
	}{
		{"off", "", "https://example.org", "https://example.org", true, true},
		{"local stack", "http://import-stub:8190", local, "http://localhost:8080", false, true},
		{"several local origins", "http://import-stub:8190", local + ",http://127.0.0.1:4321", "http://localhost:8080", false, true},
		{"public site", "http://import-stub:8190", "https://example.org", "http://localhost:8080", false, false},
		{"public second origin", "http://import-stub:8190", local + ",https://example.org", "http://localhost:8080", false, false},
		{"public api url", "http://import-stub:8190", local, "https://api.example.org", false, false},
		{"secure cookies", "http://import-stub:8190", local, "http://localhost:8080", true, false},
		{"not an origin", "import-stub", local, "http://localhost:8080", false, false},
		{"odd scheme", "ftp://import-stub", local, "http://localhost:8080", false, false},
	} {
		err := Config{
			ImportStubOrigin: tc.stub, AllowedOrigin: tc.origins, PublicURL: tc.publicURL, CookieSecure: tc.secure,
		}.checkImportStub()
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}
