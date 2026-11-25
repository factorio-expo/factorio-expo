package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/staff"
)

// The access level runs before the input is read: a missing session answers
// 401 even for a request that would fail validation.
func TestRequiredAuthPrecedesValidation(t *testing.T) {
	t.Parallel()
	for _, target := range []string{
		"/api/v1/me/favorites?limit=0",
		"/api/v1/me/favorites?bogus=1",
	} {
		response := httptest.NewRecorder()
		serveRoutes(&server{}, response, httptest.NewRequest(http.MethodGet, target, nil))
		var problem Problem
		if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if response.Code != http.StatusUnauthorized || problem.Code != "unauthorized" {
			t.Errorf("%s = %d %s", target, response.Code, response.Body)
		}
	}
	response := httptest.NewRecorder()
	serveRoutes(&server{}, response, httptest.NewRequest(http.MethodPost, "/api/v1/entries/abcdefghijkl/favorite", nil))
	if response.Code != http.StatusUnauthorized {
		t.Errorf("favorite without a session = %d", response.Code)
	}
}

// Staff operations check the token first, then the body: a schema failure is
// 422 validation_failed.
func TestAdminOperationsCheckTokenThenBody(t *testing.T) {
	t.Parallel()
	handler := adminTestHandler(t)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+testStaffToken)
		request.Header.Set(staff.UserHeader, "alice")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := call(http.MethodPost, "/admin/bans/abcdefghijkl/end", `{"bogus":1}`)
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" {
		t.Errorf("unknown body field = %d %s", response.Code, response.Body)
	}
	if response := adminRequest(t, http.MethodPost, "/admin/bans/abcdefghijkl/end", "", "alice"); response.Code != http.StatusUnauthorized {
		t.Errorf("ban end without a token = %d", response.Code)
	}
}
