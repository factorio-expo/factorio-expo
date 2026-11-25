package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"factorio-expo/apps/expo-api/internal/audit"
)

// The staff listener has its own Huma API and OpenAPI document
// (AdminSpec, checked in as apps/expo-api/openapi/admin.json): expo-admin's
// server is its only client. Every operation needs the staff token.

const (
	adminTitle       = "Factorio Expo staff API"
	adminDescription = "The staff API behind the admin console, served on its own internal " +
		"listener. Every request carries the shared staff token as a bearer token and the " +
		"staff member's username in the staff user header (docs/admin.md). Errors are " +
		"application/problem+json with a stable `code`."
	schemeStaff = "staff"
)

func adminConfig() huma.Config {
	config := newConfig(adminTitle, adminDescription)
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		schemeStaff: {
			Type: "http", Scheme: "bearer",
			Description: "The shared staff token, with the staff member's username in their header.",
		},
	}
	return config
}

// newAdminAPI registers the staff listener's Huma API on mux.
func newAdminAPI(mux *http.ServeMux) huma.API {
	return humago.New(mux, adminConfig())
}

// AdminSpec is the staff listener's OpenAPI document, as checked in at
// apps/expo-api/openapi/admin.json (`expo-api openapi admin`).
func AdminSpec() ([]byte, error) {
	s := &adminServer{}
	s.routes()
	spec, err := json.MarshalIndent(s.api.OpenAPI(), "", "  ")
	return append(spec, '\n'), err
}

// operation completes op with the staff check and its error responses.
func (s *adminServer) operation(op huma.Operation) huma.Operation {
	op.Middlewares = append(op.Middlewares, middleware(withRequestContext), middleware(s.requireStaff))
	op.Security = []map[string][]string{{schemeStaff: {}}}
	op.Errors = append(op.Errors, http.StatusBadRequest, http.StatusUnauthorized)
	return op
}

// staffUser is the verified staff member of an operation, as the catalog
// names them.
func staffUser(ctx context.Context) audit.Staff {
	identity, _ := staffFromContext(ctx)
	return audit.Staff{Username: identity.Username}
}

// registerHealth registers the hidden health check both listeners answer
// (the compose healthchecks call it); the document leaves it out.
func registerHealth(api huma.API, service string) {
	huma.Register(api, huma.Operation{
		OperationID: "health", Method: http.MethodGet, Path: "/healthz", Hidden: true,
		Summary: "Health check", Tags: []string{"Site"},
	}, func(context.Context, *struct{}) (*healthOutput, error) {
		return &healthOutput{Body: healthResponse{Service: service, Status: "ok"}}, nil
	})
}

type healthOutput struct {
	Body healthResponse
}
