package httpapi

import (
	"net/http"
	"testing"
)

// Every tag and reserved username route refuses a request without the token.
func TestAdminTagRoutesRefuseMissingToken(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/tags"},
		{http.MethodPost, "/admin/tag-groups"},
		{http.MethodPost, "/admin/tag-groups/abcDEF123456"},
		{http.MethodPost, "/admin/tag-groups/abcDEF123456/delete"},
		{http.MethodPost, "/admin/tags"},
		{http.MethodPost, "/admin/tags/abcDEF123456"},
		{http.MethodPost, "/admin/tags/abcDEF123456/delete"},
		{http.MethodPost, "/admin/tags/abcDEF123456/rules"},
		{http.MethodPost, "/admin/tags/abcDEF123456/rules/delete"},
		{http.MethodGet, "/admin/reserved-usernames"},
		{http.MethodPost, "/admin/reserved-usernames"},
		{http.MethodPost, "/admin/reserved-usernames/alice/delete"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// Wrong field types answer 422 validation_failed before the catalog runs.
func TestAdminTagSchemaFailuresAnswer422(t *testing.T) {
	cases := []struct {
		name, method, path, body string
	}{
		{"tag display name is a string", http.MethodPost, "/admin/tags", `{"display_name": 5}`},
		{"tag group position is a number", http.MethodPost, "/admin/tag-groups", `{"position": "first"}`},
		{"rule min count is a number", http.MethodPost, "/admin/tags/abcDEF123456/rules", `{"min_count": "x"}`},
		{"rule max count is a number", http.MethodPost, "/admin/tags/abcDEF123456/rules", `{"max_count": [1]}`},
		{"rule delete name is a string", http.MethodPost, "/admin/tags/abcDEF123456/rules/delete", `{"name": false}`},
		{"reserved name is a string", http.MethodPost, "/admin/reserved-usernames", `{"name": ["x"]}`},
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
