package httpapi

import (
	"net/http"
	"testing"
)

// The settings routes refuse a request without the staff token.
func TestSettingsRoutesRefuseMissingToken(t *testing.T) {
	t.Parallel()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/settings"},
		{http.MethodPost, "/admin/settings"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// The values must be an object and the reason a string; a wrong type fails
// validation before the handler runs.
func TestSettingsBodiesValidateSchema(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, body, location string
	}{
		{"values not an object", `{"values":"x"}`, "body.values"},
		{"reason not a string", `{"reason":5}`, "body.reason"},
	} {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			recorder := moderationRequest(t, http.MethodPost, "/admin/settings", route.body)
			problem := staffProblem(t, recorder)
			if recorder.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
				len(problem.Errors) != 1 || problem.Errors[0].Location != route.location {
				t.Errorf("%d %s", recorder.Code, recorder.Body)
			}
		})
	}
}

// An unknown setting key is the catalog's invalid_request, not a schema
// failure: the keys are checked against the setting definitions.
func TestSettingsUnknownKeyIsRejected(t *testing.T) {
	t.Parallel()
	recorder := moderationRequest(t, http.MethodPost, "/admin/settings", `{"values":{"no_such_setting":"x"}}`)
	if problem := staffProblem(t, recorder); recorder.Code != http.StatusBadRequest || problem.Code != "invalid_request" {
		t.Errorf("%d %s", recorder.Code, recorder.Body)
	}
}
