package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/jobs"
	"factorio-expo/apps/expo-api/internal/maintenance"
	"factorio-expo/apps/expo-api/internal/runner"
	"factorio-expo/apps/expo-api/internal/staff"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// The staff API is served on its own listener (API_ADMIN_ADDR), reachable
// only from expo-admin over an internal network; the public listener has no
// staff routes. Every request but the health check carries the shared
// staff token and the staff member's username (docs/admin.md).

type AdminDependencies struct {
	Staff   *staff.Authenticator
	Pool    *pgxpool.Pool
	Runner  *runner.Client
	Catalog *catalog.Service
	// Ops are the staff operations (tags, entries, users, bans, ...).
	Ops      *staffops.Service
	Cleanups *maintenance.Registry
	Jobs     *jobs.Service
}

type adminServer struct {
	staff    *staff.Authenticator
	queries  *db.Queries
	runner   *runner.Client
	catalog  *catalog.Service
	ops      *staffops.Service
	cleanups *maintenance.Registry
	jobs     *jobs.Service
	// api is the staff listener's Huma API (openapi_admin.go), set by routes.
	api huma.API
}

// NewAdmin builds the staff API handler. No CORS and no cookies: only
// expo-admin's server calls it, with the shared bearer token.
func NewAdmin(deps AdminDependencies) http.Handler {
	s := &adminServer{staff: deps.Staff, runner: deps.Runner, catalog: deps.Catalog, ops: deps.Ops, cleanups: deps.Cleanups, jobs: deps.Jobs}
	if deps.Pool != nil {
		s.queries = db.New(deps.Pool)
	}
	return withSecurityHeaders(s.routes())
}

func (s *adminServer) routes() http.Handler {
	serveMux := http.NewServeMux()
	s.api = newAdminAPI(serveMux)
	registerHealth(s.api, "api-admin")
	s.registerOperations(s.api)
	return withRequestLogging(serveMux)
}
