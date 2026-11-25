package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/staff"
)

// Ban bodies and the address-ban lift check their schema before the handler
// runs: a wrong type or an unparseable time is a validation_failed naming
// the field.
func TestBanBodiesValidateSchema(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, method, path, body, location string
	}{
		{"create ban actions", http.MethodPost, "/admin/bans", `{"actions":"comment"}`, "body.actions"},
		{"create ban expiry", http.MethodPost, "/admin/bans", `{"expires_at":"soon"}`, "body.expires_at"},
		{"create ban permanent", http.MethodPost, "/admin/bans", `{"permanent":"yes"}`, "body.permanent"},
		{"revoke ban reason", http.MethodPost, "/admin/bans/abcDEF123456/revoke", `{"reason":5}`, "body.reason"},
		{"change ban end", http.MethodPost, "/admin/bans/abcDEF123456/end", `{"permanent":"yes"}`, "body.permanent"},
		{"lift address ban action", http.MethodPost, "/admin/ip-bans/abcdef/lift", `{"action":5}`, "body.action"},
	} {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			recorder := moderationRequest(t, route.method, route.path, route.body)
			problem := staffProblem(t, recorder)
			if recorder.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
				len(problem.Errors) != 1 || problem.Errors[0].Location != route.location {
				t.Errorf("%d %s", recorder.Code, recorder.Body)
			}
		})
	}
}

// A ban with an unknown reason code is the catalog's invalid_request, not a
// schema failure: the field is optional in the document.
func TestCreateBanRejectsUnknownReason(t *testing.T) {
	t.Parallel()
	handler := adminDBHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/admin/bans",
		strings.NewReader(`{"username":"nobody","actions":["comment"],"reason_code":"bogus","permanent":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testStaffToken)
	request.Header.Set(staff.UserHeader, "alice")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if problem := staffProblem(t, recorder); recorder.Code != http.StatusBadRequest || problem.Code != "invalid_request" {
		t.Errorf("%d %s", recorder.Code, recorder.Body)
	}
}

// The ban and address-ban lists answer 200 with their active and ended
// groups.
func TestBanListsAnswer(t *testing.T) {
	t.Parallel()
	handler := adminDBHandler(t)
	for _, path := range []string{"/admin/bans", "/admin/bans?username=nobody", "/admin/ip-bans"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+testStaffToken)
		request.Header.Set(staff.UserHeader, "alice")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Errorf("GET %s: %d %s", path, recorder.Code, recorder.Body)
		}
	}
}
