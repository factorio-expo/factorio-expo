package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/auth"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/dataexport"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/identity"
	"factorio-expo/apps/expo-api/internal/importer"
	"factorio-expo/apps/expo-api/internal/passkey"
	"factorio-expo/apps/expo-api/internal/preview"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/ratelimit"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/apps/expo-api/internal/search"
	"factorio-expo/apps/expo-api/internal/sessions"
	"factorio-expo/apps/expo-api/internal/turnstile"
	"factorio-expo/apps/expo-api/internal/views"
)

type Dependencies struct {
	// AllowedOrigin is the comma-separated list of browser origins that may
	// call the API, including the site's own origin in production. It drives
	// both CORS and the CSRF origin check.
	AllowedOrigin string
	// SiteURL is the site's public origin (the first allowed origin): where
	// a provider sign-in sends the browser back to.
	SiteURL string
	// CookieSecure marks the session cookie Secure and gives it the __Host-
	// prefix. Only plain-HTTP local development turns it off.
	CookieSecure bool
	// ClientIPs says which proxies may name the client address.
	ClientIPs ClientIPConfig
	// Limiter enforces rate limits and the open event-stream caps; nil
	// disables both (tests).
	Limiter *ratelimit.Limiter
	Runner  *runner.Client
	Pool    *pgxpool.Pool
	// Sessions starts, checks and ends signed-in sessions.
	Sessions *sessions.Service
	// Flows signs the sign-in ceremonies' flow cookies.
	Flows *auth.FlowSigner
	// Accounts creates accounts and manages their sign-in methods.
	Accounts *accounts.Service
	// Identities are the enabled sign-in providers.
	Identities *identity.Registry
	// Turnstile checks sign-ups are human; nil turns the check off (local
	// development, e2e, tests).
	Turnstile *turnstile.Verifier
	// Passkeys runs the WebAuthn ceremonies.
	Passkeys *passkey.Service
	Catalog  *catalog.Service
	// AccountDeletion deletes accounts (DELETE /api/v1/me).
	AccountDeletion *accountdeletion.Service
	// PreviewFiles serves stored preview images.
	PreviewFiles *preview.Files
	// Progress feeds the entry event streams; the catalog and the preview
	// dispatcher publish to the same hub.
	Progress *progress.Hub
	// Renders re-renders evicted full preview files.
	Renders *preview.Renders
	Search  search.Client
	// Importer fetches the blueprints of import links; nil takes the supported
	// sites at their real addresses.
	Importer *importer.Fetcher
	// Views counts entry page views; nil ignores them (tests).
	Views *views.Counter
	// IPHashSecret keys the hashes client addresses are banned as (never
	// the addresses themselves).
	IPHashSecret string
	// RenderQueue places re-render requests in the render queue; the zero
	// value means renderqueue.Defaults().
	RenderQueue renderqueue.Policy
}

type server struct {
	allowedOrigin string
	siteURL       string
	cookieSecure  bool
	clientIPs     ClientIPConfig
	limiter       *ratelimit.Limiter
	runner        *runner.Client
	pool          *pgxpool.Pool
	queries       *db.Queries
	sessions      *sessions.Service
	flows         *auth.FlowSigner
	accounts      *accounts.Service
	identities    *identity.Registry
	passkeys      *passkey.Service
	turnstile     *turnstile.Verifier
	catalog       *catalog.Service
	deletion      *accountdeletion.Service
	exports       *dataexport.Service
	previewFiles  *preview.Files
	renders       *preview.Renders
	progress      *progress.Hub
	streams       *streamSlots
	search        search.Client
	importer      *importer.Fetcher
	views         *views.Counter
	ipHashKey     []byte
	importTokens  *importTokens
	renderQueue   renderqueue.Policy
	// api is the public listener's Huma API (openapi.go), set by routes.
	api huma.API
}

// New builds the API handler: security headers, CORS, the per-IP rate limit
// and the CSRF origin check wrap the route table. The raw exchange-string
// routes get the security headers, open CORS and their own rate limit only
// (raw.go).
func New(deps Dependencies) http.Handler {
	queries := db.New(deps.Pool)
	s := &server{
		allowedOrigin: deps.AllowedOrigin,
		siteURL:       deps.SiteURL,
		cookieSecure:  deps.CookieSecure,
		clientIPs:     deps.ClientIPs,
		limiter:       deps.Limiter,
		runner:        deps.Runner,
		pool:          deps.Pool,
		queries:       queries,
		sessions:      deps.Sessions,
		flows:         deps.Flows,
		accounts:      deps.Accounts,
		identities:    deps.Identities,
		passkeys:      deps.Passkeys,
		turnstile:     deps.Turnstile,
		catalog:       deps.Catalog,
		deletion:      deps.AccountDeletion,
		exports:       dataexport.New(queries),
		previewFiles:  deps.PreviewFiles,
		renders:       deps.Renders,
		progress:      deps.Progress,
		search:        deps.Search,
		importer:      deps.Importer,
		views:         deps.Views,
		ipHashKey:     []byte(deps.IPHashSecret),
		importTokens:  newImportTokens([]byte(deps.IPHashSecret)),
		renderQueue:   deps.RenderQueue,
	}
	if s.importer == nil {
		s.importer = importer.Default(maxSubmissionBytes)
	}
	if s.renderQueue == (renderqueue.Policy{}) {
		s.renderQueue = renderqueue.Defaults()
	}
	if deps.Limiter != nil {
		s.streams = newStreamSlots()
	}
	api := withSecurityHeaders(s.withCORS(s.withGlobalLimit(s.withOriginCheck(s.routes()))))
	rawMux := http.NewServeMux()
	rawPaths := s.rawRoutes(newRawAPI(rawMux, s.api))
	raw := withSecurityHeaders(withPublicCORS(withRequestLogging(withRawMethods(rawMux))))
	return withRawDispatch(rawPaths, raw, api)
}
