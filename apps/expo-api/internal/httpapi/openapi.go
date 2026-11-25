package httpapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"path"
	"reflect"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"factorio-expo/apps/expo-api/internal/blueprint"
	"factorio-expo/apps/expo-api/internal/catalog"
)

// The public listener's operations are Huma operations: their Go input and
// output types are the API's contract, and the OpenAPI document generated
// from them (PublicSpec, checked in as apps/expo-api/openapi/public.json)
// feeds the frontend types and the read API docs. Routes not yet moved to
// Huma stay plain mux routes and are missing from the document.

const (
	apiTitle   = "Factorio Expo API"
	apiVersion = "0.0.1"
	// apiDescription heads the generated document and the docs page.
	apiDescription = "The Factorio Expo API. The operations in the docs (search, " +
		"entries and their blueprints, raw exchange strings, preview images, tags and " +
		"mods) are the read API tools may rely on; the rest (marked x-internal) serve " +
		"the site and may change without notice. Reads are rate limited per IP (429 " +
		"with Retry-After); be gentle and cache. Entries that are private, deleted or " +
		"expired answer 404. Errors are application/problem+json with a stable `code`."
)

// publicPrefix is the path every public operation lives under (the API's major
// version): registerOperations registers them on a Huma group with it, and the
// raw routes build their paths from it. A breaking change ships as /api/v2.
const publicPrefix = "/api/v1"

// Security scheme names of the document.
const (
	schemeBearer  = "bearer"
	schemeSession = "session"
)

// apiConfig is the Huma configuration of the public listener: OpenAPI 3.1,
// no served spec or docs routes (expo-web serves both), no $schema links,
// unknown query parameters and body fields rejected.
func apiConfig() huma.Config {
	return newConfig(apiTitle, apiDescription)
}

// newConfig is the configuration both listeners' Huma APIs share.
func newConfig(title, description string) huma.Config {
	config := huma.DefaultConfig(title, apiVersion)
	config.Info.Description = description
	config.Servers = []*huma.Server{{URL: "/", Description: "The site's own origin"}}
	config.OpenAPIPath, config.DocsPath, config.SchemasPath = "", "", ""
	registry := huma.NewMapRegistry("#/components/schemas/", schemaName)
	registry.RegisterTypeAlias(reflect.TypeFor[catalog.PlannerSettings](), reflect.TypeFor[plannerSettings]())
	registerEnums(registry)
	config.Components.Schemas = registry
	config.CreateHooks = nil
	config.Transformers = []huma.Transformer{emptySlices}
	config.RejectUnknownQueryParameters = true
	return config
}

// publicSecuritySchemes are the public listener's ways to sign in.
func publicSecuritySchemes() map[string]*huma.SecurityScheme {
	return map[string]*huma.SecurityScheme{
		schemeBearer: {
			Type: "http", Scheme: "bearer",
			Description: "A session token (API clients and the site's server side).",
		},
		schemeSession: {
			Type: "apiKey", In: "cookie", Name: secureSessionCookie,
			Description: "The browser's HttpOnly session cookie.",
		},
	}
}

// schemaPrefixes name the schemas of packages whose type names alone are
// too generic (progress.Change, mods.Listed); catalog's and httpapi's types
// keep their own names.
var schemaPrefixes = map[string]string{
	"analysisresult": "Analysis",
	"blueprint":      "Blueprint",
	"challenge":      "Challenge",
	"cleanup":        "Cleanup",
	"jobs":           "Job",
	"mods":           "Mod",
	"progress":       "Progress",
}

// schemaName is huma.DefaultSchemaNamer with the package prefix.
func schemaName(t reflect.Type, hint string) string {
	name := huma.DefaultSchemaNamer(t, hint)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	return schemaPrefixes[path.Base(t.PkgPath())] + name
}

// plannerSettings documents catalog.PlannerSettings: the settings of the
// node's kind of planner.
type plannerSettings struct{}

func (plannerSettings) Schema(r huma.Registry) *huma.Schema {
	return &huma.Schema{
		Description: "Upgrade settings for an upgrade_planner node, deconstruction settings for a deconstruction_planner.",
		OneOf: []*huma.Schema{
			r.Schema(reflect.TypeFor[blueprint.UpgradeSettings](), true, ""),
			r.Schema(reflect.TypeFor[blueprint.DeconstructionSettings](), true, ""),
		},
	}
}

// newAPI registers the public listener's Huma API on mux.
func newAPI(mux *http.ServeMux) huma.API {
	config := apiConfig()
	config.Components.SecuritySchemes = publicSecuritySchemes()
	return humago.New(mux, config)
}

// newRawAPI registers the raw routes' operations on their own mux, into the
// public listener's document.
func newRawAPI(mux *http.ServeMux, public huma.API) huma.API {
	config := apiConfig()
	config.Components.SecuritySchemes = publicSecuritySchemes()
	config.OpenAPI = public.OpenAPI()
	return humago.New(mux, config)
}

// PublicSpec is the public listener's OpenAPI document, as checked in at
// apps/expo-api/openapi/public.json (`expo-api openapi`).
func PublicSpec() ([]byte, error) {
	s := &server{}
	s.routes()
	s.rawRoutes(newRawAPI(http.NewServeMux(), s.api))
	spec, err := json.MarshalIndent(s.api.OpenAPI(), "", "  ")
	return append(spec, '\n'), err
}

// imageResponse documents a preview image.
func imageResponse() map[string]*huma.Response {
	return map[string]*huma.Response{"200": {
		Description: "The WebP image.",
		Content: map[string]*huma.MediaType{
			"image/webp": {Schema: &huma.Schema{Type: huma.TypeString, ContentEncoding: "binary"}},
		},
	}}
}

// eventStreamResponse documents a text/event-stream: one message per event
// name, with the JSON data its Go type describes.
func eventStreamResponse(api huma.API, events map[string]any) map[string]*huma.Response {
	registry := api.OpenAPI().Components.Schemas
	names := slices.Sorted(maps.Keys(events))
	messages := make([]*huma.Schema, 0, len(names))
	for _, name := range names {
		data := registry.Schema(reflect.TypeOf(events[name]), true, name)
		messages = append(messages, &huma.Schema{
			Title: "Event " + name,
			Type:  huma.TypeObject,
			Properties: map[string]*huma.Schema{
				"event": {Type: huma.TypeString, Const: name},
				"data":  data,
			},
			Required: []string{"event", "data"},
		})
	}
	return map[string]*huma.Response{"200": {
		Description: "The event stream.",
		Content: map[string]*huma.MediaType{
			"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeArray, Items: &huma.Schema{OneOf: messages}}},
		},
	}}
}

// access is who may call an operation; each level adds middleware and
// documents the operation's security.
type access uint8

const (
	// accessPublic: anyone; credentials are ignored.
	accessPublic access = iota
	// accessOptional: anyone; a valid session makes the caller its account.
	accessOptional
	// accessRequired: a signed-in account.
	accessRequired
	// accessFresh: a signed-in account that signed in recently (403
	// reauth_required otherwise, fresh_sign_in.go).
	accessFresh
)

// readAPI marks an operation as part of the documented read API; every
// other operation is x-internal and left out of the docs.
const readAPI = "read-api"

// operation completes op with its access middleware, rate limit, security
// and error responses. tags[0] == readAPI publishes it in the read API docs
// (under the remaining tags).
func (s *server) operation(op huma.Operation, who access, class limitClass, gates ...gate) huma.Operation {
	if len(op.Tags) > 0 && op.Tags[0] == readAPI {
		op.Tags = op.Tags[1:]
	} else {
		if op.Extensions == nil {
			op.Extensions = map[string]any{}
		}
		op.Extensions["x-internal"] = true
	}
	op.Middlewares = append(op.Middlewares, middleware(withRequestContext))
	limit := func(next http.Handler) http.Handler { return s.limited(class, next) }
	limited := class != limitNone
	switch who {
	case accessPublic:
		op.Security = []map[string][]string{}
	case accessOptional:
		op.Middlewares = append(op.Middlewares, middleware(s.withOptionalAuth))
		op.Security = []map[string][]string{{}, {schemeBearer: {}}, {schemeSession: {}}}
		op.Errors = append(op.Errors, http.StatusUnauthorized)
	case accessRequired, accessFresh:
		op.Middlewares = append(op.Middlewares, middleware(s.requireAuth))
		op.Security = []map[string][]string{{schemeBearer: {}}, {schemeSession: {}}}
		op.Errors = append(op.Errors, http.StatusUnauthorized)
	}
	if limited {
		op.Middlewares = append(op.Middlewares, middleware(limit))
	}
	if who == accessFresh {
		gates = append([]gate{s.freshGate()}, gates...)
	}
	for _, g := range gates {
		op.Middlewares = append(op.Middlewares, middleware(g.wrap))
		op.Errors = append(op.Errors, g.errors...)
	}
	if limited {
		op.Errors = append(op.Errors, http.StatusTooManyRequests)
	}
	return op
}

// middleware runs a net/http middleware around a Huma operation, so the auth
// and rate-limit middleware serve plain and Huma routes alike. The request
// the middleware passes on (with the caller's session in its context) is the
// one the operation reads.
func middleware(wrap func(http.Handler) http.Handler) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		r, w := humago.Unwrap(ctx)
		wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next(humago.NewContext(ctx.Operation(), r, w))
		})).ServeHTTP(w, r)
	}
}

// EditTokenInput is the anonymous edit token of an operation that modifies
// an entry: the query form survives a bookmarked URL, the header keeps the
// secret out of logged URLs.
type EditTokenInput struct {
	Token       string `query:"token" doc:"The entry's edit token (anonymous entries)."`
	EditTokenHd string `header:"X-Edit-Token" doc:"The entry's edit token, as a header."`
}

func (in EditTokenInput) editToken() string {
	if in.Token != "" {
		return in.Token
	}
	return in.EditTokenHd
}
