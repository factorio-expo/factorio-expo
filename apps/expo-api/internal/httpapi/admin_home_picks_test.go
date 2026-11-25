package httpapi

import (
	"net/http"
	"testing"
)

// Every home pick route refuses a request without the token.
func TestAdminHomePickRoutesRefuseMissingToken(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/home-picks"},
		{http.MethodPost, "/admin/home-picks"},
		{http.MethodPost, "/admin/home-picks/abcDEF123456"},
		{http.MethodPost, "/admin/home-picks/abcDEF123456/delete"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// Wrong field types answer 422 validation_failed before the catalog runs.
func TestAdminHomePickSchemaFailuresAnswer422(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/admin/home-picks", `{"blueprint_id": 5}`},
		{"/admin/home-picks", `{"blueprint_id": "abcDEF123456", "caption": ["x"]}`},
		{"/admin/home-picks/abcDEF123456", `{"caption": false}`},
	} {
		recorder := adminCatalogJSON(t, http.MethodPost, tc.path, tc.body)
		if recorder.Code != http.StatusUnprocessableEntity || adminCatalogProblemCode(t, recorder) != "validation_failed" {
			t.Errorf("POST %s %s = %d %s, want 422 validation_failed", tc.path, tc.body, recorder.Code, recorder.Body)
		}
	}
}
