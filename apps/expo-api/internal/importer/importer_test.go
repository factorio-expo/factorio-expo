package importer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// exchange is a stand-in exchange string: the importer only checks the "0"
// marker, the catalog decodes it.
const exchange = "0eNqrVkrKKU0tKMrMTS1WsqqOrQUAJxkFKQ=="

// stub is a stand-in site: handler answers every request, and paths records
// what was asked.
type stub struct {
	server *httptest.Server
	mu     sync.Mutex
	paths  []string
}

func (s *stub) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func newStub(t *testing.T, handler http.HandlerFunc) *stub {
	t.Helper()
	s := &stub{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.RequestURI())
		s.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(s.server.Close)
	return s
}

// fetcherFor builds a Fetcher whose every site answers from the stub, the way
// the end-to-end stack configures one (private addresses allowed).
func fetcherFor(t *testing.T, s *stub, mutate ...func(*Config)) *Fetcher {
	t.Helper()
	config := Config{
		Sites: RebaseSites(DefaultSites(), s.server.URL), MaxExchangeBytes: 1 << 20,
		Timeout: 2 * time.Second, MaxConcurrent: 4, AllowPrivateNetworks: true,
	}
	for _, change := range mutate {
		change(&config)
	}
	fetcher, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	return fetcher
}

func text(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

func jsonBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

func TestAdaptersBuildTheirRequestFromTheLink(t *testing.T) {
	gist := `{"description":"My gist","files":{"a.md":{"content":"notes"},"b.txt":{"content":"` + exchange + `"}}}`
	prints := `{"title":"Smelter","descriptionMarkdown":"Iron.","blueprintString":"` + exchange + `"}`
	codex := `{"title":"Plastic","description":"480 a minute.","versions":[{"blueprint_string":"` + exchange + `"},{"blueprint_string":"0older"}]}`
	gistID := "aa5a315d61ae9438b18d"
	docsID := "1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	for _, tc := range []struct {
		name, link, wantRequest, wantSource string
		handler                             http.HandlerFunc
		title, description                  string
	}{
		{"pastebin", "https://pastebin.com/aBcD1234", "/pastebin/raw/aBcD1234", "https://pastebin.com/aBcD1234", text(exchange), "", ""},
		{"pastebin raw", "https://pastebin.com/raw/aBcD1234", "/pastebin/raw/aBcD1234", "https://pastebin.com/aBcD1234", text(exchange), "", ""},
		{"pastebin www", "https://www.pastebin.com/aBcD1234", "/pastebin/raw/aBcD1234", "https://pastebin.com/aBcD1234", text(exchange), "", ""},
		{"hastebin", "https://hastebin.com/abcdefghij.txt", "/hastebin/raw/abcdefghij", "https://hastebin.com/abcdefghij", text(exchange), "", ""},
		{"hastebin raw", "https://hastebin.com/raw/abcdefghij", "/hastebin/raw/abcdefghij", "https://hastebin.com/abcdefghij", text(exchange), "", ""},
		{"gist with user", "https://gist.github.com/someone/" + gistID, "/gist/gists/" + gistID, "https://gist.github.com/someone/" + gistID, jsonBody(gist), "My gist", ""},
		{"gist by id", "https://gist.github.com/" + gistID, "/gist/gists/" + gistID, "https://gist.github.com/" + gistID, jsonBody(gist), "My gist", ""},
		{"github raw", "https://raw.githubusercontent.com/user/repo/main/bp/smelter.txt", "/githubraw/user/repo/main/bp/smelter.txt", "https://raw.githubusercontent.com/user/repo/main/bp/smelter.txt", text(exchange), "", ""},
		{"gitlab raw", "https://gitlab.com/group/sub/proj/-/raw/main/bp.txt", "/gitlab/group/sub/proj/-/raw/main/bp.txt", "https://gitlab.com/group/sub/proj/-/raw/main/bp.txt", text(exchange), "", ""},
		{"gitlab blob", "https://gitlab.com/group/proj/-/blob/main/bp.txt", "/gitlab/group/proj/-/raw/main/bp.txt", "https://gitlab.com/group/proj/-/blob/main/bp.txt", text(exchange), "", ""},
		{"google docs", "https://docs.google.com/document/d/" + docsID + "/edit?usp=sharing", "/googledocs/document/d/" + docsID + "/export?format=txt", "https://docs.google.com/document/d/" + docsID, text(exchange), "", ""},
		{"factorio prints", "https://factorioprints.com/view/-NabcDEF0123456789_x", "/factorioprints/blueprints/-NabcDEF0123456789_x.json", "https://factorioprints.com/view/-NabcDEF0123456789_x", jsonBody(prints), "Smelter", "Iron."},
		{"factorio codex", "https://www.factoriocodex.com/blueprints/511", "/factoriocodex/api/v1/blueprints/511", "https://www.factoriocodex.com/blueprints/511", jsonBody(codex), "Plastic", "480 a minute."},
		{"wrapped lines", "https://pastebin.com/aBcD1234", "/pastebin/raw/aBcD1234", "https://pastebin.com/aBcD1234", text(string(rune(0xFEFF)) + exchange[:10] + "\r\n" + exchange[10:] + "\n"), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub(t, tc.handler)
			got, err := fetcherFor(t, s).Fetch(context.Background(), tc.link)
			if err != nil {
				t.Fatal(err)
			}
			if paths := s.seen(); len(paths) != 1 || paths[0] != tc.wantRequest {
				t.Fatalf("requests = %v, want [%s]", paths, tc.wantRequest)
			}
			if got.Exchange != exchange || got.SourceURL != tc.wantSource ||
				got.Title != tc.title || got.Description != tc.description {
				t.Fatalf("result = %+v", got)
			}
			if source, err := fetcherFor(t, s).SourceURL(tc.link); err != nil || source != tc.wantSource {
				t.Fatalf("SourceURL = %q, %v", source, err)
			}
		})
	}
}

func TestGistTakesTheFirstFileThatIsAnExchangeString(t *testing.T) {
	file := func(name, content string) string { return fmt.Sprintf("%q:{\"content\":%q}", name, content) }
	gistLink := "https://gist.github.com/aa5a315d61ae9438b18d"
	run := func(files ...string) (Result, error) {
		s := newStub(t, jsonBody(`{"description":"","files":{`+strings.Join(files, ",")+`}}`))
		return fetcherFor(t, s).Fetch(context.Background(), gistLink)
	}
	got, err := run(file("a.md", "readme"), file("b.txt", exchange), file("c.txt", "0other"))
	if err != nil || got.Exchange != exchange {
		t.Fatalf("got %+v, %v", got, err)
	}
	// Only the first few files (by name) are looked through.
	many := []string{}
	for i := range 5 {
		many = append(many, file(fmt.Sprintf("f%d.md", i), "notes"))
	}
	if _, err := run(append(many, file("z.txt", exchange))...); !errors.Is(err, ErrNoBlueprint) {
		t.Fatalf("a blueprint in the sixth file: %v", err)
	}
	if _, err := run(file("a.md", "no blueprint here")); !errors.Is(err, ErrNoBlueprint) {
		t.Fatalf("no blueprint: %v", err)
	}
	s := newStub(t, jsonBody(`{"files":{"a.txt":{"content":"0abc","truncated":true}}}`))
	if _, err := fetcherFor(t, s).Fetch(context.Background(), gistLink); !errors.Is(err, ErrNoBlueprint) {
		t.Fatalf("a truncated file: %v", err)
	}
}

func TestGistRequestAsksGitHubTheWayItWantsAndSendsNothingElse(t *testing.T) {
	var header http.Header
	s := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		jsonBody(`{"files":{"a":{"content":"`+exchange+`"}}}`)(w, r)
	})
	if _, err := fetcherFor(t, s).Fetch(context.Background(), "https://gist.github.com/aa5a315d61ae9438b18d"); err != nil {
		t.Fatal(err)
	}
	if header.Get("Accept") != "application/vnd.github+json" || header.Get("User-Agent") != userAgent {
		t.Fatalf("headers = %v", header)
	}
	for _, name := range []string{"Authorization", "Cookie", "Referer", "Proxy-Authorization"} {
		if header.Get(name) != "" {
			t.Fatalf("sent %s", name)
		}
	}
}

// Links that must never cause a request: every spelling that could make a
// parser and the host allowlist disagree.
func TestLinksThatAreNotAnAllowlistedHostAreRefusedWithoutAnyRequest(t *testing.T) {
	s := newStub(t, text(exchange))
	fetcher := fetcherFor(t, s)
	for _, link := range []string{
		"",
		"   ",
		"0eNqrVkrKKU0tKMrMTS1WsqqOrQUAJxkFKQ==", // a plain string is not a link
		"http://pastebin.com/aBcD1234",          // not https
		"ftp://pastebin.com/aBcD1234",
		"//pastebin.com/aBcD1234",
		"pastebin.com/aBcD1234",
		"https://pastebin.com.evil.test/aBcD1234", // suffix trick
		"https://evilpastebin.com/aBcD1234",       // prefix trick
		"https://evil.test/pastebin.com/aBcD1234", // host in the path
		"https://evil.test/?u=https://pastebin.com/aBcD1234",
		"https://evil.test#@pastebin.com/aBcD1234",
		"https://pastebin.com@evil.test/aBcD1234", // userinfo
		"https://pastebin.com:pw@pastebin.com/aBcD1234",
		"https://user@pastebin.com/aBcD1234",
		"https://pastebin.com:8443/aBcD1234",        // odd port
		"https://pastebin.com:443/aBcD1234",         // any port
		"https://pastebin.com./aBcD1234",            // trailing dot
		"https://PASTEBIN.com\\@evil.test/aBcD1234", // backslash
		"https://pastebin.com\\.evil.test/aBcD1234",
		"https://127.0.0.1/aBcD1234", // IP literals
		"https://[::1]/aBcD1234",
		"https://[::ffff:127.0.0.1]/aBcD1234",
		"https://0x7f000001/aBcD1234",
		"https://2130706433/aBcD1234",
		"https://169.254.169.254/latest/meta-data/",
		"https://localhost/aBcD1234",
		"https://pastebin.com/aBcD1234\r\nHost: evil.test",
		"https://pastebin.com/aB cD1234",
		"https://pastebin.com/../etc/passwd",
		"https://pastebin.com/raw/%2e%2e",
		"https://pastebin.com/a%2fb",
		"https://xn--pastebin-xyz.com/aBcD1234",
		"https://gist.github.com/someone/not-hex-id",
		"https://gist.github.com/../aa5a315d61ae9438b18d",
		"https://raw.githubusercontent.com/user/repo",                    // too short
		"https://raw.githubusercontent.com/user/repo/main/%2e%2e/secret", // dot segments
		"https://gitlab.com/group/proj/-/tree/main/bp.txt",               // not a file link
		"https://gitlab.com/group/proj/raw/main/bp.txt",                  // no marker
		"https://docs.google.com/spreadsheets/d/1AbCdEfGhIjKlMnOpQrStUvWxYz0123456789",
		"https://docs.google.com/document/d/short/edit",
		"https://factorioprints.com/user/someone",
		"https://www.factoriocodex.com/blueprints/abc",
		"https://www.factoriocodex.com/blueprints/511/edit",
		"https://factoriobin.com/",
		"https://www.factorio.school/view/-abc",             // no adapter (410 Gone)
		"https://www.dropbox.com/scl/fi/abc/bp.txt?rlkey=k", // no adapter
		"https://api.github.com/gists/aa5a315d61ae9438b18d", // the API host is not an input host
		"https://facorio-blueprints.firebaseio.com/blueprints/x.json",
	} {
		if _, err := fetcher.Fetch(context.Background(), link); err == nil {
			t.Errorf("%q was accepted", link)
		}
		if _, err := fetcher.SourceURL(link); err == nil {
			t.Errorf("%q was accepted by SourceURL", link)
		}
	}
	if paths := s.seen(); len(paths) != 0 {
		t.Fatalf("requests were made: %v", paths)
	}
}

func TestRefusalsAreCoded(t *testing.T) {
	fetcher := fetcherFor(t, newStub(t, text(exchange)))
	for link, want := range map[string]error{
		"not a link":                      ErrInvalidLink,
		"http://pastebin.com/aBcD1234":    ErrInvalidLink,
		"https://evil.test/x":             ErrUnsupportedSite,
		"https://pastebin.com/a/b/c/d/e/": ErrUnsupportedSite,
	} {
		if _, err := fetcher.Fetch(context.Background(), link); !errors.Is(err, want) {
			t.Errorf("%q: %v, want %v", link, err, want)
		}
	}
}

func TestSiteAnswersThatAreNotAnExchangeString(t *testing.T) {
	link := "https://pastebin.com/aBcD1234"
	for name, tc := range map[string]struct {
		handler http.HandlerFunc
		want    error
	}{
		"html page": {func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>0abc"))
		}, ErrNoBlueprint},
		"no type": {func(w http.ResponseWriter, _ *http.Request) {
			w.Header()["Content-Type"] = nil
			_, _ = w.Write([]byte(exchange))
		}, ErrNoBlueprint},
		"wrong json":   {jsonBody(`{}`), ErrNoBlueprint},
		"prose":        {text("a long story about smelting"), ErrNoBlueprint},
		"empty":        {text(""), ErrNoBlueprint},
		"not found":    {func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }, ErrSourceNotFound},
		"gone":         {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGone) }, ErrSourceNotFound},
		"forbidden":    {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, ErrSourceNotFound},
		"rate limited": {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, ErrSourceRateLimited},
		"server error": {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, ErrFetchFailed},
		"teapot":       {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }, ErrFetchFailed},
		"no content":   {func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }, ErrFetchFailed},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := fetcherFor(t, newStub(t, tc.handler)).Fetch(context.Background(), link)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	t.Run("github 403 is rate limiting", func(t *testing.T) {
		s := newStub(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
		_, err := fetcherFor(t, s).Fetch(context.Background(), "https://gist.github.com/aa5a315d61ae9438b18d")
		if !errors.Is(err, ErrSourceRateLimited) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestRedirects(t *testing.T) {
	link := "https://pastebin.com/aBcD1234"
	t.Run("within the table is followed", func(t *testing.T) {
		s := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/final" {
				text(exchange)(w, r)
				return
			}
			http.Redirect(w, r, "/final", http.StatusFound)
		})
		if got, err := fetcherFor(t, s).Fetch(context.Background(), link); err != nil || got.Exchange != exchange {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	for name, target := range map[string]string{
		"private address":     "http://169.254.169.254/latest/meta-data/",
		"loopback":            "http://127.0.0.1:1/",
		"another host":        "https://evil.test/raw",
		"userinfo":            "http://user:pw@%s/final",
		"other scheme":        "https://%s/final",
		"file scheme":         "file:///etc/passwd",
		"other loopback port": "http://%s/final",
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			var other atomic.Int32
			elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				other.Add(1)
				text(exchange)(w, r)
			}))
			defer elsewhere.Close()
			s := newStub(t, nil)
			s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				location := target
				if strings.Contains(location, "%s") {
					location = fmt.Sprintf(location, strings.TrimPrefix(elsewhere.URL, "http://"))
				}
				http.Redirect(w, r, location, http.StatusFound)
			})
			_, err := fetcherFor(t, s).Fetch(context.Background(), link)
			if !errors.Is(err, ErrSourceNotFound) && !errors.Is(err, ErrFetchFailed) {
				t.Fatalf("err = %v", err)
			}
			if other.Load() != 0 {
				t.Fatal("the redirect was followed")
			}
		})
	}
	t.Run("a loop stops", func(t *testing.T) {
		s := newStub(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/again", http.StatusFound) })
		if _, err := fetcherFor(t, s).Fetch(context.Background(), link); err == nil {
			t.Fatal("followed a redirect loop")
		}
		if n := len(s.seen()); n > maxRedirects+1 {
			t.Fatalf("%d requests", n)
		}
	})
}

func TestResponseSizeLimit(t *testing.T) {
	link := "https://pastebin.com/aBcD1234"
	limit := func(c *Config) { c.MaxExchangeBytes = 1000 }
	t.Run("declared length", func(t *testing.T) {
		s := newStub(t, text("0"+strings.Repeat("a", 2*bodySlack)))
		if _, err := fetcherFor(t, s, limit).Fetch(context.Background(), link); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("streamed past the limit", func(t *testing.T) {
		s := newStub(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			chunk := []byte("0" + strings.Repeat("a", 4095))
			for range 100 {
				if _, err := w.Write(chunk); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		})
		if _, err := fetcherFor(t, s, limit).Fetch(context.Background(), link); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("an exchange string over the limit inside a small enough page", func(t *testing.T) {
		s := newStub(t, text("0"+strings.Repeat("a", 1500)))
		if _, err := fetcherFor(t, s, limit).Fetch(context.Background(), link); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("at the limit is fine", func(t *testing.T) {
		s := newStub(t, text("0"+strings.Repeat("a", 999)))
		if _, err := fetcherFor(t, s, limit).Fetch(context.Background(), link); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSlowSitesAreCutOff(t *testing.T) {
	link := "https://pastebin.com/aBcD1234"
	short := func(c *Config) { c.Timeout = 300 * time.Millisecond }
	for name, handler := range map[string]http.HandlerFunc{
		"slow headers": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(3 * time.Second):
			case <-r.Context().Done():
			}
		},
		"slow body": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			for range 30 {
				if _, err := w.Write([]byte("0")); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				select {
				case <-time.After(100 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			_, err := fetcherFor(t, newStub(t, handler), short).Fetch(context.Background(), link)
			if !errors.Is(err, ErrFetchFailed) {
				t.Fatalf("err = %v", err)
			}
			if took := time.Since(start); took > 2*time.Second {
				t.Fatalf("took %v", took)
			}
		})
	}
}

func TestNoCookiesNoProxyAndLimitedParallelism(t *testing.T) {
	link := "https://pastebin.com/aBcD1234"
	t.Run("cookies are not kept", func(t *testing.T) {
		var cookies atomic.Int32
		s := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Cookie") != "" {
				cookies.Add(1)
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "x"})
			text(exchange)(w, r)
		})
		fetcher := fetcherFor(t, s)
		for range 2 {
			if _, err := fetcher.Fetch(context.Background(), link); err != nil {
				t.Fatal(err)
			}
		}
		if cookies.Load() != 0 {
			t.Fatal("a cookie was sent back")
		}
	})
	t.Run("the environment's proxy is ignored", func(t *testing.T) {
		t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
		t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
		t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
		t.Setenv("NO_PROXY", "")
		if _, err := fetcherFor(t, newStub(t, text(exchange))).Fetch(context.Background(), link); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("too many at once is busy", func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{}, 1)
		s := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			started <- struct{}{}
			<-release
			text(exchange)(w, r)
		})
		fetcher := fetcherFor(t, s, func(c *Config) { c.MaxConcurrent = 1 })
		done := make(chan error, 1)
		go func() { _, err := fetcher.Fetch(context.Background(), link); done <- err }()
		<-started
		if _, err := fetcher.Fetch(context.Background(), link); !errors.Is(err, ErrBusy) {
			t.Fatalf("second import: %v", err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestNewChecksTheTable(t *testing.T) {
	base := func() Config {
		return Config{Sites: DefaultSites(), MaxExchangeBytes: 1 << 20}
	}
	if _, err := New(base()); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"no size":        func(c *Config) { c.MaxExchangeBytes = 0 },
		"duplicate host": func(c *Config) { c.Sites[1].Hosts = []string{"pastebin.com"} },
		"upper case":     func(c *Config) { c.Sites[0].Hosts = []string{"Pastebin.com"} },
		"bad base":       func(c *Config) { c.Sites[0].Base = "ftp://pastebin.com" },
		"userinfo base":  func(c *Config) { c.Sites[0].Base = "https://u:p@pastebin.com" },
		"no hosts":       func(c *Config) { c.Sites[0].Hosts = nil },
	} {
		config := base()
		change(&config)
		if _, err := New(config); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEveryDefaultSiteIsHttpsOnAKnownHost(t *testing.T) {
	want := map[string]string{
		"pastebin": "https://pastebin.com", "hastebin": "https://hastebin.com", "gist": "https://api.github.com",
		"githubraw": "https://raw.githubusercontent.com", "gitlab": "https://gitlab.com",
		"factoriobin": "https://factoriobin.com", "googledocs": "https://docs.google.com",
		"factorioprints": "https://facorio-blueprints.firebaseio.com",
		"factoriocodex":  "https://www.factoriocodex.com",
	}
	sites := DefaultSites()
	if len(sites) != len(want) {
		t.Fatalf("%d sites", len(sites))
	}
	for _, site := range sites {
		if site.Base != want[site.Name] {
			t.Errorf("%s base = %s", site.Name, site.Base)
		}
	}
}

func TestFactorioBinTakesTheBlueprintAndItsInfo(t *testing.T) {
	info := `{"post":{"title":"Demo post"},"node":{"name":"Example Book","description":"A test book"}}`
	for _, link := range []string{"https://factoriobin.com/post/abc_123", "https://factoriobin.com/post/abc_123/blueprint.txt"} {
		s := newStub(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/info.json") {
				jsonBody(info)(w, r)
				return
			}
			text(exchange)(w, r)
		})
		got, err := fetcherFor(t, s).Fetch(context.Background(), link)
		if err != nil {
			t.Fatal(err)
		}
		if got.Exchange != exchange || got.Title != "Example Book" || got.Description != "A test book" ||
			got.SourceURL != "https://factoriobin.com/post/abc_123" {
			t.Fatalf("result = %+v", got)
		}
		want := []string{"/factoriobin/post/abc_123/blueprint.txt", "/factoriobin/post/abc_123/info.json"}
		if paths := s.seen(); !slices.Equal(paths, want) {
			t.Fatalf("requests = %v, want %v", paths, want)
		}
	}
}

func TestFactorioBinImportsWithoutItsInfo(t *testing.T) {
	s := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/info.json") {
			http.NotFound(w, r)
			return
		}
		text(exchange)(w, r)
	})
	got, err := fetcherFor(t, s).Fetch(context.Background(), "https://factoriobin.com/post/abc_123")
	if err != nil || got.Exchange != exchange || got.Title != "" || got.Description != "" {
		t.Fatalf("result = %+v, %v", got, err)
	}
}

func TestFactorioBinMayRedirectToItsCDNOnly(t *testing.T) {
	var bin Site
	for _, site := range DefaultSites() {
		if site.Name == "factoriobin" {
			bin = site
		}
	}
	fetcher := Default(1 << 20)
	for target, want := range map[string]bool{
		"https://cdn.factoriobin.com/perma/bp/d/e/demo-da5ygo/fbin-demo-0.txt": true,
		"http://cdn.factoriobin.com/x":                                         false,
		"https://evil.factoriobin.com/x":                                       false,
		"https://cdn.factoriobin.com.evil.test/x":                              false,
	} {
		err := fetcher.checkRedirect(redirectRequest(t, bin, target), []*http.Request{{}})
		if (err == nil) != want {
			t.Errorf("redirect to %s: err = %v, want allowed %v", target, err, want)
		}
	}
}
