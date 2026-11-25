package httpapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/ratelimit"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	exchange, err := os.ReadFile("../../../../test-data/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(exchange)
}

func uploadAnonymous(t *testing.T, handler http.Handler, body map[string]any) catalog.Entry {
	t.Helper()
	created := postJSON(t, handler, "/api/v1/entries", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create entry = %d, %s", created.Code, created.Body)
	}
	var entry catalog.Entry
	if err := json.Unmarshal(created.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

// maxAge reads the max-age of a public Cache-Control header.
func maxAge(t *testing.T, cacheControl string) int {
	t.Helper()
	value, ok := strings.CutPrefix(cacheControl, "public, max-age=")
	if !ok {
		t.Fatalf("Cache-Control = %q, want public with max-age", cacheControl)
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return seconds
}

// The raw routes serve the exact strings as cacheable text/plain that any
// origin may read: the live revision, any revision, and every leaf of a book.
func TestRawStringsOfABook(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	book := readFixture(t, "book-starter-kit.txt")
	entry := uploadAnonymous(t, handler, map[string]any{"blueprint": book})
	leaves := entry.Revision.Root.Leaves()
	if len(leaves) < 2 {
		t.Fatalf("book fixture has %d leaves, want several", len(leaves))
	}

	base := "/api/v1/entries/" + entry.ID
	cases := []struct {
		path, body, filename, etag string
		maxAge                     int
	}{
		{base + "/raw", book, entry.ID + ".txt", entry.Revision.ID, 300},
		{base + "/revisions/" + entry.Revision.ID + "/raw", book,
			entry.ID + "-" + entry.Revision.ID + ".txt", entry.Revision.ID, 86400},
	}
	for _, leaf := range leaves {
		cases = append(cases, struct {
			path, body, filename, etag string
			maxAge                     int
		}{"/api/v1/blueprints/" + leaf.ID + "/raw", leaf.ExchangeString, leaf.ID + ".txt", leaf.ID, 86400})
	}
	for _, c := range cases {
		// A page on another site, and one on our own origin, get the same
		// response: one cached copy serves both.
		for _, origin := range []string{"", "https://fbe.example.test", "http://localhost:3000"} {
			request := authedRequest(http.MethodGet, c.path, "", nil)
			if origin != "" {
				request.Header.Set("Origin", origin)
			}
			response := serve(handler, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, %s", c.path, response.Code, response.Body)
			}
			if response.Body.String() != c.body {
				t.Errorf("GET %s body differs from the stored string", c.path)
			}
			want := map[string]string{
				"Content-Type":                     "text/plain; charset=utf-8",
				"Content-Disposition":              `inline; filename="` + c.filename + `"`,
				"ETag":                             `"` + c.etag + `"`,
				"Access-Control-Allow-Origin":      "*",
				"Access-Control-Allow-Credentials": "",
				"Vary":                             "",
				"Cross-Origin-Resource-Policy":     "cross-origin",
				"X-Content-Type-Options":           "nosniff",
			}
			for header, value := range want {
				if got := response.Header().Get(header); got != value {
					t.Errorf("GET %s (origin %q) %s = %q, want %q", c.path, origin, header, got, value)
				}
			}
			if got := maxAge(t, response.Header().Get("Cache-Control")); got != c.maxAge {
				t.Errorf("GET %s max-age = %d, want %d", c.path, got, c.maxAge)
			}
		}

		conditional := authedRequest(http.MethodGet, c.path, "", nil)
		conditional.Header.Set("If-None-Match", `"`+c.etag+`"`)
		if code := serve(handler, conditional).Code; code != http.StatusNotModified {
			t.Errorf("conditional GET %s = %d, want 304", c.path, code)
		}
		head := serve(handler, authedRequest(http.MethodHead, c.path, "", nil))
		if head.Code != http.StatusOK || head.Body.Len() != 0 {
			t.Errorf("HEAD %s = %d with %d bytes", c.path, head.Code, head.Body.Len())
		}
	}

	// A leaf id under the wrong entry, or a revision of another entry, is
	// unknown; so is a bad id. Errors stay readable cross-origin.
	other := uploadAnonymous(t, handler, map[string]any{"blueprint": readFixture(t, "bp-starter-smelter.txt")})
	for _, path := range []string{
		"/api/v1/entries/" + other.ID + "/revisions/" + entry.Revision.ID + "/raw",
		"/api/v1/blueprints/not-an-id/raw",
		"/api/v1/entries/AAAAAAAAAAAA/raw",
	} {
		response := serve(handler, authedRequest(http.MethodGet, path, "", nil))
		if response.Code != http.StatusNotFound || response.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("GET %s = %d (ACAO %q), want 404 open to every origin",
				path, response.Code, response.Header().Get("Access-Control-Allow-Origin"))
		}
		if cache := response.Header().Get("Cache-Control"); cache != "private, no-cache" {
			t.Errorf("GET %s 404 Cache-Control = %q, want private", path, cache)
		}
	}

	// Only reads: a preflight is answered, a write is refused.
	preflight := authedRequest(http.MethodOptions, base+"/raw", "", nil)
	preflight.Header.Set("Origin", "https://fbe.example.test")
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	response := serve(handler, preflight)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight = %d (ACAO %q)", response.Code, response.Header().Get("Access-Control-Allow-Origin"))
	}
	if code := serve(handler, authedRequest(http.MethodPost, base+"/raw", "", nil)).Code; code != http.StatusMethodNotAllowed {
		t.Errorf("POST raw = %d, want 405", code)
	}
}

// Raw strings follow the signed-out view: unlisted entries are served, private
// ones are not, not even to their owner.
func TestRawStringsFollowVisibility(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	ownerToken, _ := registerTestUser(t, handler, "raw-owner")
	for visibility, want := range map[string]int{"unlisted": http.StatusOK, "private": http.StatusNotFound} {
		entry := createEntryWithVisibility(t, handler, ownerToken, visibility)
		for _, path := range []string{
			"/api/v1/entries/" + entry.ID + "/raw",
			"/api/v1/entries/" + entry.ID + "/revisions/" + entry.Revision.ID + "/raw",
			"/api/v1/blueprints/" + entry.Revision.Root.Leaves()[0].ID + "/raw",
		} {
			for name, token := range map[string]string{"anonymous": "", "owner": ownerToken} {
				if code := serve(handler, authedRequest(http.MethodGet, path, token, nil)).Code; code != want {
					t.Errorf("%s GET %s of a %s entry = %d, want %d", name, path, visibility, code, want)
				}
			}
		}
	}
}

// Caches never keep an expiring entry's string past its expiry.
func TestRawStringCacheEndsAtExpiry(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPI(t)
	entry := uploadAnonymous(t, handler, map[string]any{
		"blueprint": readFixture(t, "bp-starter-smelter.txt"), "expires_in_days": 1,
	})
	response := serve(handler, authedRequest(http.MethodGet,
		"/api/v1/entries/"+entry.ID+"/revisions/"+entry.Revision.ID+"/raw", "", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET raw = %d", response.Code)
	}
	if got := maxAge(t, response.Header().Get("Cache-Control")); got >= 86400 || got < 86000 {
		t.Errorf("max-age = %d, want just under a day", got)
	}
}

// The raw routes skip the global per-IP budget for their own larger one: the
// editor's proxy fetches for all its users from one address.
func TestRawStringsHaveTheirOwnRateLimit(t *testing.T) {
	truncateSocialFixtures(t)
	handler, _ := newTestAPIWith(t, func(deps *httpapi.Dependencies) {
		deps.Limiter = ratelimit.New(1000)
	})
	entry := uploadAnonymous(t, handler, map[string]any{"blueprint": readFixture(t, "bp-starter-smelter.txt")})
	get := func(path string) int {
		request := authedRequest(http.MethodGet, path, "", nil)
		request.RemoteAddr = "203.0.113.9:1234"
		return serve(handler, request).Code
	}
	raw := "/api/v1/blueprints/" + entry.Revision.Root.Leaves()[0].ID + "/raw"
	// 600 burst, plus what refills (20/s) while the loop runs.
	served := 0
	for get(raw) == http.StatusOK && served < 1000 {
		served++
	}
	if served < 600 || served >= 700 {
		t.Fatalf("raw reads served before a 429 = %d, want just over 600", served)
	}
	// The other routes' budgets were not touched.
	if code := get("/api/v1/entries/" + entry.ID); code != http.StatusOK {
		t.Fatalf("entry read after raw reads = %d, want 200", code)
	}
}
