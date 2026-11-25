// Package importer fetches an exchange string from a link to another site, for
// the upload form's import option. It is the only code that makes the API
// fetch a URL a user supplied, so it is deliberately narrow:
//
//   - A fixed table of hosts (Site), each with its own adapter. A link whose
//     host (parsed, matched exactly) is not in it is refused unfetched.
//   - The outgoing URL is the adapter's base plus a path built from id
//     characters the adapter validated, never the pasted text.
//   - The connection itself refuses non-public addresses at connect time
//     (guard.go), so DNS answers cannot walk it to the internal network.
//   - One client without proxy, cookies or credentials, an overall timeout, a
//     response size cap, redirects only to the table's own hosts (plus the
//     one content host an adapter names) and few of them, an expected content
//     type per adapter, and a cap on parallel imports.
//   - A fetch budget per site and hour over all callers (budget.go), so the
//     site cannot be made to spend a third party's rate limit, and a short
//     cache of successful resolves (cache.go).
//
// docs/api.md#import-from-other-sites lists the sites.
package importer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Defaults of Config.
const (
	DefaultTimeout       = 10 * time.Second
	DefaultMaxConcurrent = 8
	maxRedirects         = 3
	// bodySlack is what a JSON wrapper may add around an exchange string.
	bodySlack = 64 << 10
	userAgent = "factorio-expo-importer/1"
)

// Config builds a Fetcher.
type Config struct {
	// Sites are the supported sites (DefaultSites).
	Sites []Site
	// MaxExchangeBytes is the largest exchange string accepted (the upload
	// body limit); a response may be a little larger to hold its wrapper.
	MaxExchangeBytes int64
	// Timeout is the hard limit of one fetch, redirects and body included.
	Timeout time.Duration
	// MaxConcurrent caps imports in flight; more answer ErrBusy.
	MaxConcurrent int
	// HourlyBudget is how many fetches an hour each site gets over all callers
	// (DefaultHourlyBudget); SiteBudgets overrides it by Site.Name. Past it
	// Fetch answers ErrSiteBusy until tokens refill.
	HourlyBudget int
	SiteBudgets  map[string]int
	// CacheTTL, CacheMaxEntries and CacheMaxBytes bound the cache of successful
	// resolves (the defaults of cache.go).
	CacheTTL        time.Duration
	CacheMaxEntries int
	CacheMaxBytes   int64
	// Now is the clock (time.Now); tests set their own.
	Now func() time.Time
	// AllowPrivateNetworks lifts the connect-time address and port check, for
	// the stand-in server of a local or end-to-end stack (config.go). Never
	// set it where users can import.
	AllowPrivateNetworks bool
}

// Result is a fetched blueprint.
type Result struct {
	// Exchange is the exchange string.
	Exchange string
	// SourceURL is the canonical https link of the page it came from.
	SourceURL string
	// Title and Description are suggestions from sites that have them.
	Title, Description string
}

// Fetcher resolves links through the site table.
type Fetcher struct {
	byHost       map[string]Site
	redirectable map[string]string
	client       *http.Client
	maxExchange  int64
	slots        chan struct{}
	budgets      map[string]*budget
	cache        *cache
	now          func() time.Time
}

// redirectRule names the one extra host a site's requests may be redirected
// to: the https host its export answers from, matched by a fixed pattern of
// the host name alone.
type redirectRule struct {
	scheme string
	match  func(host string) bool
}

// siteContextKey carries the Site a request belongs to, so the client's
// redirect check knows which adapter's extra host applies.
type siteContextKey struct{}

// errRedirectRefused is a redirect off the table's hosts or past the limit.
var errRedirectRefused = errors.New("import: redirect refused")

// New checks the site table and builds a Fetcher.
func New(config Config) (*Fetcher, error) {
	if config.MaxExchangeBytes <= 0 {
		return nil, errors.New("importer: MaxExchangeBytes must be positive")
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = DefaultMaxConcurrent
	}
	if config.HourlyBudget <= 0 {
		config.HourlyBudget = DefaultHourlyBudget
	}
	if config.CacheTTL <= 0 {
		config.CacheTTL = DefaultCacheTTL
	}
	if config.CacheMaxEntries <= 0 {
		config.CacheMaxEntries = DefaultCacheMaxEntries
	}
	if config.CacheMaxBytes <= 0 {
		config.CacheMaxBytes = DefaultCacheMaxBytes
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	fetcher := &Fetcher{
		byHost: map[string]Site{}, redirectable: map[string]string{},
		maxExchange: config.MaxExchangeBytes, slots: make(chan struct{}, config.MaxConcurrent),
		budgets: map[string]*budget{}, now: config.Now,
		cache: newCache(config.CacheTTL, config.CacheMaxEntries, config.CacheMaxBytes),
	}
	for _, site := range config.Sites {
		base, err := url.Parse(site.Base)
		if err != nil || base.Host == "" || base.User != nil || (base.Scheme != "https" && base.Scheme != "http") {
			return nil, fmt.Errorf("importer: site %s has no valid base URL", site.Name)
		}
		if len(site.Hosts) == 0 || site.plan == nil {
			return nil, fmt.Errorf("importer: site %s has no hosts or no adapter", site.Name)
		}
		for _, host := range site.Hosts {
			if host != strings.ToLower(host) {
				return nil, fmt.Errorf("importer: host %s of %s is not lower-case", host, site.Name)
			}
			if _, taken := fetcher.byHost[host]; taken {
				return nil, fmt.Errorf("importer: host %s is in the table twice", host)
			}
			fetcher.byHost[host] = site
		}
		fetcher.redirectable[base.Host] = base.Scheme
		perHour := config.HourlyBudget
		if own, ok := config.SiteBudgets[site.Name]; ok && own > 0 {
			perHour = own
		}
		fetcher.budgets[site.Name] = newBudget(perHour, config.Now())
	}
	control := guardAddress
	if config.AllowPrivateNetworks {
		control = nil
	}
	fetcher.client = &http.Client{
		Transport:     newTransport(control),
		Timeout:       config.Timeout,
		CheckRedirect: fetcher.checkRedirect,
		// No cookie jar: nothing is kept or sent between requests.
	}
	return fetcher, nil
}

// Default is a Fetcher over the supported sites at their real addresses, with
// the default limits.
func Default(maxExchangeBytes int64) *Fetcher {
	fetcher, err := New(Config{Sites: DefaultSites(), MaxExchangeBytes: maxExchangeBytes})
	if err != nil {
		// The built-in table is checked by a test; this cannot happen.
		panic(err)
	}
	return fetcher
}

// checkRedirect follows a redirect only to a host of the table (same scheme)
// or the extra host of the request's own adapter, without credentials, a few
// times.
func (f *Fetcher) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: too many redirects", errRedirectRefused)
	}
	if req.URL.User != nil {
		return fmt.Errorf("%w: credentials", errRedirectRefused)
	}
	if scheme, ok := f.redirectable[req.URL.Host]; ok && scheme == req.URL.Scheme {
		return nil
	}
	if site, ok := req.Context().Value(siteContextKey{}).(Site); ok {
		rule := site.redirect
		if rule.match != nil && req.URL.Scheme == rule.scheme && rule.match(req.URL.Host) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", errRedirectRefused, req.URL.Host)
}

// resolve matches a link to its site and builds the plan; nothing is fetched.
func (f *Fetcher) resolve(rawLink string) (Site, plan, error) {
	link, err := parseLink(rawLink)
	if err != nil {
		return Site{}, plan{}, err
	}
	site, ok := f.byHost[link.host]
	if !ok {
		return Site{}, plan{}, ErrUnsupportedSite.Withf("links from %s are not supported", link.host)
	}
	built, err := site.plan(link.segments)
	if err != nil {
		return Site{}, plan{}, ErrUnsupportedSite.Withf("that %s link is not one of the shapes we can import", link.host)
	}
	return site, built, nil
}

// SourceURL is the canonical link of a pasted link that one of the adapters
// takes, without fetching it: what an upload stores as its source.
func (f *Fetcher) SourceURL(rawLink string) (string, error) {
	site, built, err := f.resolve(rawLink)
	if err != nil {
		return "", err
	}
	return "https://" + site.Hosts[0] + built.source, nil
}

// Fetch resolves a link: the exchange string at it, and where it came from.
func (f *Fetcher) Fetch(ctx context.Context, rawLink string) (Result, error) {
	site, built, err := f.resolve(rawLink)
	if err != nil {
		return Result{}, err
	}
	source := "https://" + site.Hosts[0] + built.source
	if cached, ok := f.cache.get(source, f.now()); ok {
		return cached, nil
	}
	select {
	case f.slots <- struct{}{}:
		defer func() { <-f.slots }()
	default:
		return Result{}, ErrBusy
	}
	if !f.budgets[site.Name].take(f.now()) {
		slog.Warn("import budget spent", "site", site.Name)
		return Result{}, ErrSiteBusy
	}
	body, err := f.get(ctx, site, built)
	if err != nil {
		return Result{}, err
	}
	found, err := built.extract(body)
	if err != nil {
		return Result{}, err
	}
	if int64(len(found.exchange)) > f.maxExchange {
		return Result{}, ErrResponseTooLarge
	}
	if built.details != "" && found.title == "" && found.description == "" {
		// Best effort: the blueprint is already in hand.
		if info, err := f.get(ctx, site, plan{target: built.details, accept: mediaJSON}); err == nil {
			found.title, found.description = built.detailsExtract(info)
		}
	}
	result := Result{
		Exchange: found.exchange, SourceURL: source,
		Title: found.title, Description: found.description,
	}
	f.cache.put(source, result, f.now())
	return result, nil
}

// get requests the plan's URL and returns its body, within the limits.
func (f *Fetcher) get(ctx context.Context, site Site, built plan) ([]byte, error) {
	ctx = context.WithValue(ctx, siteContextKey{}, site)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, site.Base+built.target, nil)
	if err != nil {
		return nil, ErrFetchFailed
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", built.accept)
	if built.github {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, errRedirectRefused) {
			// A private Google document redirects to its sign-in page.
			return nil, ErrSourceNotFound
		}
		slog.Warn("import fetch failed", "site", site.Name, "error", err)
		return nil, ErrFetchFailed
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, ErrSourceNotFound
	case resp.StatusCode == http.StatusTooManyRequests || (built.github && resp.StatusCode == http.StatusForbidden):
		return nil, ErrSourceRateLimited
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrSourceNotFound
	default:
		slog.Warn("import fetch status", "site", site.Name, "status", resp.StatusCode)
		return nil, ErrFetchFailed
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mediaType != built.accept {
		return nil, ErrNoBlueprint
	}
	limit := f.maxExchange + bodySlack
	if resp.ContentLength > limit {
		return nil, ErrResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		slog.Warn("import read failed", "site", site.Name, "error", err)
		return nil, ErrFetchFailed
	}
	if int64(len(body)) > limit {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}
