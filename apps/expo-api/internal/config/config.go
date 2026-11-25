// Package config reads expo-api's settings from the environment (names are
// listed in .env.example) and enforces the rules that
// tie them together. Load takes the lookup function, so tests pass a map and
// no other package reads the environment.
package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"factorio-expo/apps/expo-api/internal/auth"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/httpapi"
	"factorio-expo/apps/expo-api/internal/importer"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/apps/expo-api/internal/staff"
)

// MaxIPBanDuration caps API_IP_BAN_DURATION: addresses rotate, so an address
// ban is meant to last hours.
const MaxIPBanDuration = 24 * time.Hour

// DefaultTrustedProxies covers loopback and private ranges: in compose the
// API port is published on 127.0.0.1 only, so the peers that can reach it
// are the reverse proxy (via the docker gateway) and expo-web (SSR).
const DefaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// Config is everything expo-api reads at startup.
type Config struct {
	HTTPAddr  string
	RunnerURL string
	// AllowedOrigin is API_ALLOWED_ORIGIN as written; Origins is its list,
	// whose first entry is SiteURL, the site's own origin.
	AllowedOrigin string
	Origins       []string
	SiteURL       string
	// PublicURL is the API's browser-facing base, for provider callbacks.
	PublicURL   string
	DatabaseURL string
	JWTSecret   string
	// JWTSecretPrevious is the retired API_JWT_SECRET: it only verifies,
	// during a rotation.
	JWTSecretPrevious string
	// IPHashSecret keys the address hashes of IP bans and (a key derived
	// from it) the ban holds of deleted accounts; IPBanDuration is how long
	// one lasts.
	IPHashSecret  string
	IPBanDuration time.Duration
	CookieSecure  bool
	ClientIPs     httpapi.ClientIPConfig
	RateLimit     bool
	PollInterval  time.Duration
	// SearchPollInterval is how often the search-index outbox is polled.
	SearchPollInterval time.Duration
	// ViewFlushInterval is how often counted page views reach PostgreSQL;
	// SearchViewInterval how often changed counts reach the search index.
	ViewFlushInterval  time.Duration
	SearchViewInterval time.Duration
	// QuickPreviewTTL is how long a quick preview lives.
	QuickPreviewTTL time.Duration
	// ChallengeScoresTTL is how long a challenge's scored board is reused; 0
	// turns the cache off.
	ChallengeScoresTTL time.Duration
	// TagMinScore is the score a tag needs to be suggested on upload.
	TagMinScore int
	// RenderQueue orders and caps render work (API_RENDER_*).
	RenderQueue renderqueue.Policy
	// StorageDir holds the API's files (preview images); in compose a host
	// bind mount (EXPO_STORAGE_DIR).
	StorageDir string
	// DeletedUsersFile is the ledger of hard-deleted account ids
	// (API_DELETED_USERS_FILE): kept outside the database and its dumps, and
	// replayed after a restore (docs/admin.md#account-deletion).
	DeletedUsersFile string
	// Encoders caps parallel preview encodes (0: half the CPUs).
	Encoders int
	// StorageMaxBytes caps the preview files; StorageMinFreeBytes is the
	// free space the storage disk keeps (0 turns either off).
	StorageMaxBytes     int64
	StorageMinFreeBytes int64
	TypesenseURL        string
	TypesenseAPIKey     string
	// AdminAddr is the staff API listener (API_ADMIN_ADDR); empty serves no
	// staff routes at all. AdminToken is the shared token expo-admin sends.
	AdminAddr  string
	AdminToken string
	// TurnstileSecret turns the sign-up human check on; empty leaves it off.
	TurnstileSecret string
	// CloudflareToken and CloudflareZone enable the CDN cache purge; both
	// are needed.
	CloudflareToken string
	CloudflareZone  string
	// ImportStubOrigin (API_IMPORT_STUB_ORIGIN) points every import site at
	// a stand-in server and lets imports reach private addresses: for the
	// e2e stack and local work only. Load refuses it unless the site itself
	// is local.
	ImportStubOrigin string
	// ImportHourlyBudget (API_IMPORT_HOURLY_BUDGET) is how many fetches per
	// hour the API makes from each import site, summed over all callers;
	// ImportGistHourlyBudget (API_IMPORT_GIST_HOURLY_BUDGET) is GitHub's,
	// whose unauthenticated API allows 60 an hour per address.
	ImportHourlyBudget     int
	ImportGistHourlyBudget int
}

// Load reads the configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	l := loader{getenv: getenv}
	cfg := Config{
		HTTPAddr:          l.str("API_HTTP_ADDR", ":8080"),
		RunnerURL:         l.str("PREVIEW_RUNNER_URL", "http://localhost:8081"),
		AllowedOrigin:     l.str("API_ALLOWED_ORIGIN", "http://localhost:3000,http://localhost:4321"),
		PublicURL:         l.str("API_PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:       getenv("DATABASE_URL"),
		JWTSecret:         getenv("API_JWT_SECRET"),
		JWTSecretPrevious: getenv("API_JWT_SECRET_PREVIOUS"),
		IPHashSecret:      getenv("API_IP_HASH_SECRET"),
		TypesenseURL:      l.str("TYPESENSE_URL", "http://expo-typesense:8108"),
		TypesenseAPIKey:   getenv("TYPESENSE_API_KEY"),
		StorageDir:        l.str("API_STORAGE_DIR", "/var/lib/expo/storage"),
		DeletedUsersFile:  l.str("API_DELETED_USERS_FILE", "/var/lib/expo/ledger/deleted-users.log"),
		AdminAddr:         getenv("API_ADMIN_ADDR"),
		AdminToken:        getenv("API_ADMIN_TOKEN"),
		TurnstileSecret:   getenv("API_TURNSTILE_SECRET_KEY"),
		CloudflareToken:   getenv("API_CLOUDFLARE_API_TOKEN"),
		CloudflareZone:    getenv("API_CLOUDFLARE_ZONE_ID"),
		ImportStubOrigin:  getenv("API_IMPORT_STUB_ORIGIN"),
		CookieSecure:      l.str("API_COOKIE_SECURE", "true") != "false",
		RateLimit:         l.str("API_RATE_LIMIT", "true") != "false",
	}

	cfg.Encoders = l.integer("API_PREVIEW_ENCODERS", 0, func(n int) bool { return n >= 0 }, "must be 0 (half the CPUs) or more")
	cfg.ImportHourlyBudget = l.integer("API_IMPORT_HOURLY_BUDGET", importer.DefaultHourlyBudget,
		func(n int) bool { return n >= 1 }, "must be 1 or more")
	cfg.ImportGistHourlyBudget = l.integer("API_IMPORT_GIST_HOURLY_BUDGET", importer.DefaultGistHourlyBudget,
		func(n int) bool { return n >= 1 }, "must be 1 or more")
	cfg.TagMinScore = l.integer("API_TAG_SUGGESTION_MIN_SCORE", catalog.DefaultTagSuggestionMinScore,
		func(n int) bool { return n >= 1 }, "must be 1 or more")
	cfg.PollInterval = l.duration("API_PREVIEW_POLL_INTERVAL", 3*time.Second, nil, "")
	cfg.SearchPollInterval = l.duration("API_SEARCH_POLL_INTERVAL", 5*time.Second, nil, "")
	cfg.ViewFlushInterval = l.duration("API_VIEW_FLUSH_INTERVAL", time.Minute, positive, "is not a positive duration")
	cfg.SearchViewInterval = l.duration("API_SEARCH_VIEW_INTERVAL", 10*time.Minute, positive, "is not a positive duration")
	cfg.QuickPreviewTTL = l.duration("API_QUICK_PREVIEW_TTL", 24*time.Hour, positive, "is not a positive duration")
	cfg.ChallengeScoresTTL = l.duration("API_CHALLENGE_SCORES_TTL", catalog.DefaultChallengeScoresTTL,
		func(d time.Duration) bool { return d >= 0 }, "must not be negative (0 turns the cache off)")
	cfg.IPBanDuration = l.duration("API_IP_BAN_DURATION", catalog.DefaultIPBanDuration,
		func(d time.Duration) bool { return d > 0 && d <= MaxIPBanDuration },
		fmt.Sprintf("must be a duration up to %s", MaxIPBanDuration))
	cfg.StorageMaxBytes = l.bytes("API_STORAGE_MAX_BYTES", "150GiB")
	cfg.StorageMinFreeBytes = l.bytes("API_STORAGE_MIN_FREE_BYTES", "20GiB")
	cfg.RenderQueue = l.renderQueue()
	if l.err != nil {
		return Config{}, l.err
	}

	if err := cfg.checkSecrets(); err != nil {
		return Config{}, err
	}
	trusted, err := httpapi.ParseTrustedProxies(l.str("API_TRUSTED_PROXIES", DefaultTrustedProxies))
	if err != nil {
		return Config{}, fmt.Errorf("API_TRUSTED_PROXIES: %w", err)
	}
	cfg.ClientIPs = httpapi.ClientIPConfig{TrustedProxies: trusted, Header: l.str("API_CLIENT_IP_HEADER", "X-Real-IP")}

	if err := cfg.checkAdmin(); err != nil {
		return Config{}, err
	}
	if err := cfg.checkSite(); err != nil {
		return Config{}, err
	}
	if err := cfg.checkImportStub(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// checkSecrets enforces the rules of the secrets: present, long enough, and
// distinct, so a leak of one never grants the other.
func (c Config) checkSecrets() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if err := auth.ValidateSecret(c.JWTSecret); err != nil {
		return fmt.Errorf("API_JWT_SECRET %w", err)
	}
	if c.JWTSecretPrevious != "" {
		if err := auth.ValidateSecret(c.JWTSecretPrevious); err != nil {
			return fmt.Errorf("API_JWT_SECRET_PREVIOUS %w", err)
		}
		if c.JWTSecretPrevious == c.JWTSecret {
			return fmt.Errorf("API_JWT_SECRET_PREVIOUS must differ from API_JWT_SECRET")
		}
	}
	if err := auth.ValidateSecret(c.IPHashSecret); err != nil {
		return fmt.Errorf("API_IP_HASH_SECRET %w", err)
	}
	if c.JWTSecret == c.IPHashSecret {
		return fmt.Errorf("API_JWT_SECRET and API_IP_HASH_SECRET must differ")
	}
	return checkTypesenseKey(c.TypesenseAPIKey, c.AllowedOrigin)
}

// checkAdmin: without API_ADMIN_ADDR there is no admin listener; with it,
// API_ADMIN_TOKEN is required and the listener must not share the public one.
func (c Config) checkAdmin() error {
	if c.AdminAddr == "" {
		return nil
	}
	if len(c.AdminToken) < staff.MinTokenLength {
		return fmt.Errorf("API_ADMIN_ADDR needs API_ADMIN_TOKEN of at least %d characters", staff.MinTokenLength)
	}
	if c.AdminAddr == c.HTTPAddr {
		return fmt.Errorf("API_ADMIN_ADDR must differ from API_HTTP_ADDR")
	}
	return nil
}

// loader reads typed values and keeps the first error, so Load reads
// straight down.
type loader struct {
	getenv func(string) string
	err    error
}

func (l *loader) fail(format string, args ...any) {
	if l.err == nil {
		l.err = fmt.Errorf(format, args...)
	}
}

func (l *loader) str(key, fallback string) string {
	if value := l.getenv(key); value != "" {
		return value
	}
	return fallback
}

// integer reads an int; ok says whether the value is allowed, and what is the
// rest of the error message.
func (l *loader) integer(key string, fallback int, ok func(int) bool, what string) int {
	value, err := strconv.Atoi(l.str(key, strconv.Itoa(fallback)))
	if err != nil || !ok(value) {
		l.fail("%s %s", key, what)
	}
	return value
}

func positive(d time.Duration) bool { return d > 0 }

func (l *loader) duration(key string, fallback time.Duration, ok func(time.Duration) bool, what string) time.Duration {
	value, err := time.ParseDuration(l.str(key, fallback.String()))
	if err != nil {
		l.fail("%s is not a duration: %v", key, err)
		return fallback
	}
	if ok != nil && !ok(value) {
		l.fail("%s %s", key, what)
	}
	return value
}

// bytes reads a byte size such as "150GiB" or "20 GB" (or a plain number of
// bytes).
func (l *loader) bytes(key, fallback string) int64 {
	size, err := humanize.ParseBytes(l.str(key, fallback))
	if err != nil || size > math.MaxInt64 {
		l.fail("%s is not a byte size such as 150GiB", key)
		return 0
	}
	return int64(size)
}

// renderQueue reads the render queue's tuning (API_RENDER_*); unset values
// keep renderqueue.Defaults().
func (l *loader) renderQueue() renderqueue.Policy {
	policy := renderqueue.Defaults()
	for _, count := range []struct {
		env    string
		target *int
	}{
		{"API_RENDER_WINDOW", &policy.Window},
		{"API_RENDER_CAP_ANONYMOUS", &policy.CapAnonymous},
		{"API_RENDER_CAP_SIGNED_IN", &policy.CapSignedIn},
	} {
		*count.target = l.integer(count.env, *count.target, func(n int) bool { return n >= 1 }, "must be 1 or more")
	}
	for _, delay := range []struct {
		env    string
		target *time.Duration
	}{
		{"API_RENDER_DELAY_SIGNED_IN", &policy.DelaySignedIn},
		{"API_RENDER_DELAY_ANONYMOUS", &policy.DelayAnonymous},
		{"API_RENDER_DELAY_RERENDER", &policy.DelayRerender},
		{"API_RENDER_DELAY_REPAIR", &policy.DelayRepair},
		{"API_RENDER_LEAF_GAP", &policy.LeafGap},
	} {
		*delay.target = l.duration(delay.env, *delay.target,
			func(d time.Duration) bool { return d >= 0 }, "is not a duration of 0 or more")
	}
	return policy
}

func splitOrigins(list string) []string {
	var origins []string
	for origin := range strings.SplitSeq(list, ",") {
		if origin = strings.TrimRight(strings.TrimSpace(origin), "/"); origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}
