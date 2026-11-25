package httpapi

import (
	"net/http"
	"testing"
)

// The failed jobs routes refuse a request without the staff token.
func TestAdminJobRoutesRefuseMissingToken(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/jobs/failed"},
		{http.MethodPost, "/admin/jobs/preview/abcDEF123456/rerun"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// A job kind outside the closed set answers 422 validation_failed.
func TestAdminJobKindIsClosed(t *testing.T) {
	cases := []struct {
		name, method, path, body string
	}{
		{"list filter", http.MethodGet, "/admin/jobs/failed?kind=bogus", ""},
		{"rerun kind", http.MethodPost, "/admin/jobs/bogus/abcDEF123456/rerun", `{"reason": "x"}`},
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
