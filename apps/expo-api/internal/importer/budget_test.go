package importer

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// clock is a settable test clock.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newClock() *clock { return &clock{now: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)} }

func TestSiteBudgetIsSharedByEveryLinkOfTheSiteAndRefills(t *testing.T) {
	s := newStub(t, nil)
	body := `{"files":{"a.txt":{"content":"` + exchange + `"}}}`
	s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/gist/") {
			jsonBody(body)(w, r)
			return
		}
		text(exchange)(w, r)
	})
	clk := newClock()
	fetcher := fetcherFor(t, s, func(c *Config) {
		c.Now = clk.Now
		c.HourlyBudget = 100
		c.SiteBudgets = map[string]int{"gist": 3}
	})
	gist := func(i int) string {
		return "https://gist.github.com/user/" + strings.Repeat("a", 7) + string(rune('0'+i))
	}

	// Three different gists spend the gist budget; the fourth is refused
	// without a request to GitHub, whoever asks.
	for i := range 3 {
		if _, err := fetcher.Fetch(context.Background(), gist(i)); err != nil {
			t.Fatalf("gist %d: %v", i, err)
		}
	}
	before := len(s.seen())
	if _, err := fetcher.Fetch(context.Background(), gist(3)); !errors.Is(err, ErrSiteBusy) {
		t.Fatalf("fourth gist: %v, want ErrSiteBusy", err)
	}
	if len(s.seen()) != before {
		t.Fatal("a request went out with the budget spent")
	}
	// Other sites have their own budget.
	if _, err := fetcher.Fetch(context.Background(), "https://pastebin.com/aBcD1234"); err != nil {
		t.Fatalf("pastebin: %v", err)
	}
	// A third of the hour gives one token back.
	clk.advance(20 * time.Minute)
	if _, err := fetcher.Fetch(context.Background(), gist(3)); err != nil {
		t.Fatalf("after refill: %v", err)
	}
	if _, err := fetcher.Fetch(context.Background(), gist(4)); !errors.Is(err, ErrSiteBusy) {
		t.Fatalf("second after refill: %v, want ErrSiteBusy", err)
	}
}

func TestBudgetDefaults(t *testing.T) {
	now := newClock().Now()
	b := newBudget(DefaultGistHourlyBudget, now)
	for i := range DefaultGistHourlyBudget {
		if !b.take(now) {
			t.Fatalf("token %d refused", i)
		}
	}
	if b.take(now) {
		t.Fatal("more than the hourly budget in one instant")
	}
}

func TestRepeatedLinksAreServedFromTheCacheForAFewMinutes(t *testing.T) {
	s := newStub(t, text(exchange))
	clk := newClock()
	fetcher := fetcherFor(t, s, func(c *Config) { c.Now = clk.Now; c.CacheTTL = 5 * time.Minute })
	for _, link := range []string{
		"https://pastebin.com/aBcD1234", "https://www.pastebin.com/raw/aBcD1234?x=1", "https://pastebin.com/aBcD1234#top",
	} {
		got, err := fetcher.Fetch(context.Background(), link)
		if err != nil || got.Exchange != exchange || got.SourceURL != "https://pastebin.com/aBcD1234" {
			t.Fatalf("%s: %+v, %v", link, got, err)
		}
	}
	if n := len(s.seen()); n != 1 {
		t.Fatalf("%d fetches for one canonical source, want 1", n)
	}
	// A different source is its own fetch.
	if _, err := fetcher.Fetch(context.Background(), "https://pastebin.com/zZzZ9999"); err != nil || len(s.seen()) != 2 {
		t.Fatalf("other source: %v, %d fetches", err, len(s.seen()))
	}
	clk.advance(5*time.Minute + time.Second)
	if _, err := fetcher.Fetch(context.Background(), "https://pastebin.com/aBcD1234"); err != nil || len(s.seen()) != 3 {
		t.Fatalf("after expiry: %v, %d fetches", err, len(s.seen()))
	}
}

func TestCacheHitsCostNoBudgetAndFailuresAreNotCached(t *testing.T) {
	s := newStub(t, nil)
	fail := true
	s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.NotFound(w, r)
			return
		}
		text(exchange)(w, r)
	})
	fetcher := fetcherFor(t, s, func(c *Config) { c.HourlyBudget = 2 })
	link := "https://pastebin.com/aBcD1234"
	if _, err := fetcher.Fetch(context.Background(), link); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("missing: %v", err)
	}
	fail = false
	for range 5 {
		if _, err := fetcher.Fetch(context.Background(), link); err != nil {
			t.Fatalf("after the page appeared: %v", err)
		}
	}
	// One failed and one successful fetch spent the budget of 2; the later
	// reads came out of the cache.
	if _, err := fetcher.Fetch(context.Background(), "https://pastebin.com/other123"); !errors.Is(err, ErrSiteBusy) {
		t.Fatalf("budget: %v, want ErrSiteBusy", err)
	}
}

func TestCacheIsBoundedInEntriesAndBytes(t *testing.T) {
	now := newClock().Now()
	c := newCache(time.Minute, 3, 100)
	for i, key := range []string{"a", "b", "c", "d"} {
		c.put(key, Result{Exchange: "0x"}, now.Add(time.Duration(i)*time.Second))
	}
	if _, ok := c.get("a", now.Add(5*time.Second)); ok {
		t.Fatal("the oldest entry survived the entry cap")
	}
	if len(c.items) != 3 {
		t.Fatalf("%d entries", len(c.items))
	}
	c.put("big", Result{Exchange: strings.Repeat("0", 101)}, now)
	if _, ok := c.get("big", now); ok {
		t.Fatal("an entry past the byte cap was kept")
	}
	c.put("fat", Result{Exchange: strings.Repeat("0", 90)}, now.Add(10*time.Second))
	if c.bytes > 100 {
		t.Fatalf("%d bytes held, cap 100", c.bytes)
	}
}

func TestDocsRedirectHostIsMatchedNarrowly(t *testing.T) {
	for host, want := range map[string]bool{
		"doc-0k-0s-docs.googleusercontent.com":           true,
		"doc-14-78-docstext.googleusercontent.com":       true,
		"doc-14-20-docs.googleusercontent.com":           true,
		"docs.googleusercontent.com":                     false,
		"googleusercontent.com":                          false,
		"evil.googleusercontent.com":                     false,
		"lh3.googleusercontent.com":                      false,
		"doc-0k-0s-docs.googleusercontent.com.evil":      false,
		"evil.test/doc-0k-0s-docs.googleusercontent.com": false,
		"x.doc-0k-0s-docs.googleusercontent.com":         false,
		"doc-0k-0s-docs.googleusercontent.com:8443":      false,
		"accounts.google.com":                            false,
		"doc.google.com":                                 false,
	} {
		if got := isDocsContentHost(host); got != want {
			t.Errorf("isDocsContentHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func redirectRequest(t *testing.T, site Site, target string) *http.Request {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), siteContextKey{}, site)
	return (&http.Request{URL: u}).WithContext(ctx)
}

func TestOnlyTheDocsAdapterMayFollowAContentHostRedirect(t *testing.T) {
	fetcher := fetcherFor(t, newStub(t, text(exchange)))
	var docs, pastebin Site
	for _, site := range DefaultSites() {
		switch site.Name {
		case "googledocs":
			docs = site
		case "pastebin":
			pastebin = site
		}
	}
	const content = "https://doc-0k-0s-docs.googleusercontent.com/export/abc"
	for _, tc := range []struct {
		name   string
		site   Site
		target string
		hops   int
		ok     bool
	}{
		{"docs to its content host", docs, content, 1, true},
		{"docs, http", docs, "http://doc-0k-0s-docs.googleusercontent.com/export/abc", 1, false},
		{"docs, userinfo", docs, "https://user:pw@doc-0k-0s-docs.googleusercontent.com/x", 1, false},
		{"docs, wildcard google host", docs, "https://anything.google.com/x", 1, false},
		{"docs, other googleusercontent host", docs, "https://lh3.googleusercontent.com/x", 1, false},
		{"docs, too many hops", docs, content, maxRedirects, false},
		{"pastebin to the docs content host", pastebin, content, 1, false},
	} {
		via := make([]*http.Request, tc.hops)
		err := fetcher.checkRedirect(redirectRequest(t, tc.site, tc.target), via)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok %v", tc.name, err, tc.ok)
		}
	}
}

// TestDocsRedirectThroughTheStub follows a redirect from the docs adapter to a
// second server: allowed when the adapter names that host, refused when not.
func TestDocsRedirectThroughTheStub(t *testing.T) {
	content := httptest.NewServer(text(exchange))
	defer content.Close()
	contentHost := strings.TrimPrefix(content.URL, "http://")
	s := newStub(t, nil)
	s.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, content.URL+"/export", http.StatusTemporaryRedirect)
	})
	build := func(rule redirectRule) *Fetcher {
		sites := RebaseSites(DefaultSites(), s.server.URL)
		for i := range sites {
			if sites[i].Name == "googledocs" {
				sites[i].redirect = rule
			}
		}
		fetcher, err := New(Config{Sites: sites, MaxExchangeBytes: 1 << 20, Timeout: 2 * time.Second, AllowPrivateNetworks: true})
		if err != nil {
			t.Fatal(err)
		}
		return fetcher
	}
	link := "https://docs.google.com/document/d/" + strings.Repeat("a", 30) + "/edit"

	allowed := redirectRule{scheme: "http", match: func(host string) bool { return host == contentHost }}
	if got, err := build(allowed).Fetch(context.Background(), link); err != nil || got.Exchange != exchange {
		t.Fatalf("allowed host: %+v, %v", got, err)
	}
	if _, err := build(redirectRule{}).Fetch(context.Background(), link); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("no rule: %v, want ErrSourceNotFound", err)
	}
	wrongScheme := redirectRule{scheme: "https", match: allowed.match}
	if _, err := build(wrongScheme).Fetch(context.Background(), link); !errors.Is(err, ErrSourceNotFound) {
		t.Fatalf("wrong scheme: %v, want ErrSourceNotFound", err)
	}
}
