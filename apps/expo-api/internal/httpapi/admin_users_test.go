package httpapi

import (
	"net/http"
	"testing"
)

// Every user route refuses a request without the staff token.
func TestUserRoutesRefuseMissingToken(t *testing.T) {
	t.Parallel()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/users"},
		{http.MethodGet, "/admin/users/deletion-queue"},
		{http.MethodGet, "/admin/users/abcDEF123456"},
		{http.MethodPost, "/admin/users/abcDEF123456/logout"},
		{http.MethodPost, "/admin/users/abcDEF123456/clear-profile"},
		{http.MethodPost, "/admin/users/abcDEF123456/delete"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// A reason of the wrong type fails validation before the handler runs.
func TestUserBodiesValidateSchema(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, path string
	}{
		{"logout", "/admin/users/abcDEF123456/logout"},
		{"clear profile", "/admin/users/abcDEF123456/clear-profile"},
		{"delete", "/admin/users/abcDEF123456/delete"},
	} {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			recorder := moderationRequest(t, http.MethodPost, route.path, `{"reason":5}`)
			problem := staffProblem(t, recorder)
			if recorder.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
				len(problem.Errors) != 1 || problem.Errors[0].Location != "body.reason" {
				t.Errorf("%d %s", recorder.Code, recorder.Body)
			}
		})
	}
}

// Deleting an account needs a reason: the catalog answers its own code when
// it is missing (no schema failure).
func TestDeleteUserNeedsReason(t *testing.T) {
	t.Parallel()
	recorder := moderationRequest(t, http.MethodPost, "/admin/users/abcDEF123456/delete", `{}`)
	if problem := staffProblem(t, recorder); recorder.Code != http.StatusBadRequest || problem.Code != "invalid_request" {
		t.Errorf("%d %s", recorder.Code, recorder.Body)
	}
}

// The account list's status and cursor are checked by the catalog before
// any database read.
func TestUserListFiltersAreChecked(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/admin/users?status=nope", "/admin/users?cursor=nope"} {
		recorder := adminRequest(t, http.MethodGet, path, testStaffToken, "alice")
		if problem := staffProblem(t, recorder); recorder.Code != http.StatusBadRequest || problem.Code != "invalid_request" {
			t.Errorf("GET %s: %d %s", path, recorder.Code, recorder.Body)
		}
	}
}
