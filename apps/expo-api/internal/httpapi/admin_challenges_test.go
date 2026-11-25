package httpapi

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// Every challenge route refuses a request without the staff token.
func TestChallengeRoutesRefuseMissingToken(t *testing.T) {
	t.Parallel()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/admin/challenges"},
		{http.MethodGet, "/admin/challenges/params"},
		{http.MethodPost, "/admin/challenges"},
		{http.MethodGet, "/admin/challenges/abcDEF123456"},
		{http.MethodPost, "/admin/challenges/abcDEF123456"},
		{http.MethodPost, "/admin/challenges/abcDEF123456/publish"},
		{http.MethodPost, "/admin/challenges/abcDEF123456/unpublish"},
		{http.MethodPost, "/admin/challenges/abcDEF123456/delete"},
		{http.MethodPost, "/admin/challenges/abcDEF123456/submissions/abcDEF123456/verdict"},
	} {
		if got := adminRequest(t, route.method, route.path, "", "alice").Code; got != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status %d, want 401", route.method, route.path, got)
		}
	}
}

// The parameter list answers without the database: the parameters are the
// same for every challenge.
func TestChallengeParamsAnswer(t *testing.T) {
	t.Parallel()
	recorder := adminRequest(t, http.MethodGet, "/admin/challenges/params", testStaffToken, "alice")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"rate_out"`) {
		t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
}

// Challenge bodies check their schema before the handler runs: a wrong type
// is a validation_failed naming the field.
func TestChallengeBodiesValidateSchema(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, method, path, body, location string
	}{
		{"create slug", http.MethodPost, "/admin/challenges", `{"slug":5}`, "body.slug"},
		{"create votes per user", http.MethodPost, "/admin/challenges", `{"votes_per_user":"x"}`, "body.votes_per_user"},
		{"create rules", http.MethodPost, "/admin/challenges", `{"rules":"x"}`, "body.rules"},
		{"create start", http.MethodPost, "/admin/challenges", `{"starts_at":"soon"}`, "body.starts_at"},
		{"update title", http.MethodPost, "/admin/challenges/abcDEF123456", `{"title":5}`, "body.title"},
		{"verdict", http.MethodPost,
			"/admin/challenges/abcDEF123456/submissions/abcDEF123456/verdict", `{"verdict":5}`, "body.verdict"},
	} {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			recorder := moderationRequest(t, route.method, route.path, route.body)
			problem := staffProblem(t, recorder)
			if recorder.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
				!slices.ContainsFunc(problem.Errors, func(detail *huma.ErrorDetail) bool {
					return detail.Location == route.location
				}) {
				t.Errorf("%d %s", recorder.Code, recorder.Body)
			}
		})
	}
}

// The catalog checks what the schema leaves optional and answers its own
// invalid_request, not a schema failure.
func TestChallengeCatalogChecksKeepTheirCode(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, path, body string
	}{
		{"create with a bad slug", "/admin/challenges", `{"slug":"Not Valid","title":"x","rules":{"content":"vanilla","kind":"any","goal":{"param":"x","target":{},"direction":"max"},"restrictions":[]},"starts_at":"2030-01-01T00:00:00Z","ends_at":"2030-01-02T00:00:00Z","voting_ends_at":"2030-01-03T00:00:00Z"}`},
		{"verdict without a known value", "/admin/challenges/abcDEF123456/submissions/abcDEF123456/verdict",
			`{"verdict":"maybe","reason":"x"}`},
	} {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			recorder := moderationRequest(t, http.MethodPost, route.path, route.body)
			if problem := staffProblem(t, recorder); recorder.Code != http.StatusBadRequest || problem.Code != "invalid_request" {
				t.Errorf("%d %s", recorder.Code, recorder.Body)
			}
		})
	}
}
