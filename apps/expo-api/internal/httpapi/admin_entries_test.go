package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/staff"
)

// adminCatalogJSON sends a staff request, as expo-admin does, with a JSON body.
func adminCatalogJSON(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testStaffToken)
	request.Header.Set(staff.UserHeader, "alice")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	adminTestHandler(t).ServeHTTP(recorder, request)
	return recorder
}

// adminCatalogProblemCode is the stable code of a problem+json answer.
func adminCatalogProblemCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var problem Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v (body %s)", err, recorder.Body.String())
	}
	return problem.Code
}

// Every entry route refuses a request without the staff token.
func TestAdminEntryRoutesRefuseMissingToken(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/entries"},
		{http.MethodGet, "/admin/entries/abcDEF123456"},
		{http.MethodGet, "/admin/entries/abcDEF123456/revisions/abcDEF123456/raw"},
		{http.MethodPost, "/admin/entries/abcDEF123456/restore"},
		{http.MethodPost, "/admin/entries/abcDEF123456/reindex"},
		{http.MethodPost, "/admin/entries/abcDEF123456/purge-cache"},
		{http.MethodPost, "/admin/entries/abcDEF123456/rerun"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// A schema failure answers 422 validation_failed before the catalog runs.
func TestAdminEntrySchemaFailuresAnswer422(t *testing.T) {
	cases := []struct {
		name, method, path, body string
	}{
		{"min_leaves is a number", http.MethodGet, "/admin/entries?min_leaves=many", ""},
		{"failed is a boolean", http.MethodGet, "/admin/entries?failed=maybe", ""},
		{"rerun phase is a string", http.MethodPost, "/admin/entries/abcDEF123456/rerun", `{"phase": 3}`},
		{"restore reason is a string", http.MethodPost, "/admin/entries/abcDEF123456/restore", `{"reason": 7}`},
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
