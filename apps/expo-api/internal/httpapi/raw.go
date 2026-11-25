package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"factorio-expo/apps/expo-api/internal/catalog"
)

// The raw routes serve bare exchange strings to anyone, other sites included
// (the blueprint editor fetches them through its own proxy, so all its users
// share one address). They ignore cookies, answer only for entries a
// signed-out visitor may see, and are open to every origin. They skip the
// global per-IP limit and draw from their own, larger budget instead.

// Cache lifetimes: a revision and a leaf never change, the live revision of an
// entry can. Both are cut short when the entry expires.
const (
	rawCacheAge     = 24 * time.Hour
	rawLiveCacheAge = 5 * time.Minute
)

// The raw routes' paths.
const (
	rawEntryPath     = publicPrefix + "/entries/{entryID}/raw"
	rawRevisionPath  = publicPrefix + "/entries/{entryID}/revisions/{revisionID}/raw"
	rawBlueprintPath = publicPrefix + "/blueprints/{blueprintID}/raw"
)

type rawRevisionInput struct {
	EntryPath
	RevisionID string `path:"revisionID" doc:"The revision's id."`
}

// rawRoutes registers the raw routes as operations of api on their own mux,
// and answers a path-only mux the dispatcher in New matches them by (so it
// also catches their preflights and wrong methods).
func (s *server) rawRoutes(api huma.API) *http.ServeMux {
	textResponse := func(description string) map[string]*huma.Response {
		return map[string]*huma.Response{"200": {
			Description: description,
			Content:     map[string]*huma.MediaType{"text/plain": {Schema: &huma.Schema{Type: huma.TypeString}}},
		}}
	}
	operation := func(id, path, summary, description string) huma.Operation {
		op := s.operation(huma.Operation{
			OperationID: id, Method: http.MethodGet, Path: path,
			Tags: []string{readAPI, "Raw strings"}, Summary: summary, Description: description,
			Responses: textResponse("The exchange string."),
			// Other sites fetch these, cache-busting parameters and all.
			SkipValidateParams: true,
		}, accessPublic, limitRaw)
		return op
	}
	huma.Register(api, operation("getEntryRaw", rawEntryPath,
		"The live revision's exchange string as uploaded",
		"text/plain, byte for byte what the author submitted (a book stays a book). Open to every "+
			"origin (CORS *). Shared-cacheable for up to 5 minutes. Only public and unlisted entries "+
			"and unexpired quick previews answer."),
		func(ctx context.Context, in *EntryPath) (*huma.StreamResponse, error) {
			raw, err := s.catalog.LiveRevisionRaw(ctx, in.EntryID)
			return serveRaw(raw, in.EntryID, rawLiveCacheAge, err)
		})
	huma.Register(api, operation("getRevisionRaw", rawRevisionPath,
		"A revision's exchange string as uploaded",
		"Like the entry's raw string, for one revision (immutable, cached for a day)."),
		func(ctx context.Context, in *rawRevisionInput) (*huma.StreamResponse, error) {
			raw, err := s.catalog.RevisionRaw(ctx, in.EntryID, in.RevisionID)
			return serveRaw(raw, in.EntryID+"-"+in.RevisionID, rawCacheAge, err)
		})
	huma.Register(api, operation("getBlueprintRaw", rawBlueprintPath,
		"One blueprint's own exchange string",
		"The leaf's own self-contained string, also for a blueprint inside a book."),
		func(ctx context.Context, in *previewInput) (*huma.StreamResponse, error) {
			raw, err := s.catalog.BlueprintRaw(ctx, in.BlueprintID)
			return serveRaw(raw, in.BlueprintID, rawCacheAge, err)
		})

	paths := http.NewServeMux()
	for _, path := range []string{rawEntryPath, rawRevisionPath, rawBlueprintPath} {
		paths.Handle(path, http.NotFoundHandler())
	}
	return paths
}

// serveRaw writes the string as text/plain, cacheable by browsers and
// CDNs. filename names the download ("<id>.txt").
func serveRaw(raw catalog.RawString, filename string, cacheAge time.Duration, err error) (*huma.StreamResponse, error) {
	if err != nil {
		return nil, err
	}
	return &huma.StreamResponse{Body: func(ctx huma.Context) {
		r, w := humago.Unwrap(ctx)
		header := w.Header()
		header.Set("Content-Type", "text/plain; charset=utf-8")
		header.Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s.txt"`, filename))
		header.Set("ETag", `"`+raw.SourceID+`"`)
		header.Set("Cache-Control", fmt.Sprintf("public, max-age=%d", rawMaxAge(cacheAge, raw.ExpiresAt, time.Now())))
		http.ServeContent(w, r, "", time.Time{}, strings.NewReader(raw.ExchangeString))
	}}, nil
}

// rawMaxAge is the cache lifetime in seconds: cacheAge, cut to the time left
// before the entry expires.
func rawMaxAge(cacheAge time.Duration, expiresAt *time.Time, now time.Time) int {
	age := cacheAge
	if expiresAt != nil {
		age = min(age, expiresAt.Sub(now))
	}
	return max(int(age/time.Second), 0)
}

// withPublicCORS opens a response to every origin. No credentials: the raw
// routes ignore cookies, and one header for all origins keeps a cached copy
// valid for each of them (no Vary: Origin).
func withPublicCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Access-Control-Allow-Origin", "*")
		header.Set("Cross-Origin-Resource-Policy", "cross-origin")
		if r.Method == http.MethodOptions {
			header.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			header.Set("Access-Control-Allow-Headers", "*")
			header.Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withRawMethods admits reads only; preflights never get here.
func withRawMethods(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD, OPTIONS")
			httpError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withRawDispatch sends requests for a raw route to raw and everything else
// to api.
func withRawDispatch(rawPaths *http.ServeMux, raw, api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := rawPaths.Handler(r); pattern != "" {
			raw.ServeHTTP(w, r)
			return
		}
		api.ServeHTTP(w, r)
	})
}
