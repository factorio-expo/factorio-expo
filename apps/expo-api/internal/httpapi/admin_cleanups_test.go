package httpapi

import (
	"net/http"
	"testing"
)

// The cleanup routes refuse a request without the staff token.
func TestAdminCleanupRoutesRefuseMissingToken(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/cleanups"},
		{http.MethodPost, "/admin/cleanups/deleted_entries"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// A non-numeric target size answers 422 validation_failed before the cleanup runs.
func TestAdminCleanupSchemaFailuresAnswer422(t *testing.T) {
	cases := []struct {
		name, method, path, body string
	}{
		{"overview target is a number", http.MethodGet, "/admin/cleanups?target_bytes=lots", ""},
		{"run target is a number", http.MethodPost, "/admin/cleanups/full_previews", `{"target_bytes": "x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := adminCatalogJSON(t, tc.method, tc.path, tc.body)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422 (body %s)", recorder.Code, recorder.Body.String())
			}
			if code := adminCatalogProblemCode(t, recorder); code != "validation_failed" {
				t.Fatalf("code %q, want validation_failed", code)
			}
		})
	}
}
