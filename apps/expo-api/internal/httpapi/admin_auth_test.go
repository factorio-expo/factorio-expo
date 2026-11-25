package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/staff"
)

var testStaffToken = strings.Repeat("t", staff.MinTokenLength)

func adminTestHandler(t *testing.T) http.Handler {
	t.Helper()
	authenticator, err := staff.NewAuthenticator(testStaffToken)
	if err != nil {
		t.Fatal(err)
	}
	return NewAdmin(AdminDependencies{Staff: authenticator})
}

func adminRequest(t *testing.T, method, path, token, username string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if username != "" {
		request.Header.Set(staff.UserHeader, username)
	}
	recorder := httptest.NewRecorder()
	adminTestHandler(t).ServeHTTP(recorder, request)
	return recorder
}

func TestAdminRequiresStaffToken(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		token    string
		username string
		want     int
	}{
		{"no token", "/admin/badges", "", "alice", http.StatusUnauthorized},
		{"wrong token", "/admin/badges", strings.Repeat("x", staff.MinTokenLength), "alice", http.StatusUnauthorized},
		{"token prefix", "/admin/badges", testStaffToken[:10], "alice", http.StatusUnauthorized},
		{"no username", "/admin/badges", testStaffToken, "", http.StatusBadRequest},
		{"control characters", "/admin/badges", testStaffToken, "ali\x01ce", http.StatusBadRequest},
		{"health is open", "/healthz", "", "", http.StatusOK},
		{"public routes absent", "/api/v1/entries/abc", testStaffToken, "alice", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := adminRequest(t, http.MethodGet, tc.path, tc.token, tc.username).Code; got != tc.want {
				t.Fatalf("status %d, want %d", got, tc.want)
			}
		})
	}
}

// Every staff route refuses a request without the token before doing anything.
func TestAdminRoutesRefuseMissingToken(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/reports/entries"},
		{http.MethodGet, "/admin/reports/comments"},
		{http.MethodGet, "/admin/bans"},
		{http.MethodGet, "/admin/ip-bans"},
		{http.MethodGet, "/admin/badges"},
		{http.MethodPost, "/admin/entries/abcDEF123456/delete"},
		{http.MethodGet, "/admin/cleanups"},
		{http.MethodPost, "/admin/cleanups/deleted_entries"},
		{http.MethodGet, "/admin/audit"},
		{http.MethodGet, "/admin/users"},
		{http.MethodGet, "/admin/users/abcDEF123456"},
		{http.MethodPost, "/admin/users/abcDEF123456/logout"},
		{http.MethodPost, "/admin/users/abcDEF123456/delete"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

func TestAdminSecurityHeaders(t *testing.T) {
	recorder := adminRequest(t, http.MethodGet, "/healthz", "", "")
	if recorder.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal("security headers missing")
	}
}
