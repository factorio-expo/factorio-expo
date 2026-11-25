package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/staff"
)

// The status filter is an enum: any other value is a validation_failed
// naming the query parameter.
func TestProblemReportsFilterValidatesStatus(t *testing.T) {
	t.Parallel()
	recorder := moderationRequest(t, http.MethodGet, "/admin/problem-reports?status=bogus", "")
	problem := staffProblem(t, recorder)
	if recorder.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
		len(problem.Errors) != 1 || problem.Errors[0].Location != "query.status" {
		t.Errorf("%d %s", recorder.Code, recorder.Body)
	}
}

func TestProblemReportBodiesValidateSchema(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, path, body, location string
	}{
		{"resolve status", "/admin/problem-reports/abcDEF123456/resolve", `{"status":1}`, "body.status"},
		{"set note", "/admin/problem-reports/abcDEF123456/note", `{"note":1}`, "body.note"},
		{"ban reporter days", "/admin/problem-reports/abcDEF123456/ban-reporter", `{"days":"x"}`, "body.days"},
	} {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			recorder := moderationRequest(t, http.MethodPost, route.path, route.body)
			problem := staffProblem(t, recorder)
			if recorder.Code != http.StatusUnprocessableEntity || problem.Code != "validation_failed" ||
				len(problem.Errors) != 1 || problem.Errors[0].Location != route.location {
				t.Errorf("%d %s", recorder.Code, recorder.Body)
			}
		})
	}
}

// Setting the note of a report that does not exist is the catalog's
// not_found.
func TestProblemReportNoteUnknownReport(t *testing.T) {
	t.Parallel()
	handler := adminDBHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/admin/problem-reports/abcDEF123456/note",
		strings.NewReader(`{"note":"checked"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testStaffToken)
	request.Header.Set(staff.UserHeader, "alice")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if problem := staffProblem(t, recorder); recorder.Code != http.StatusNotFound || problem.Code != "not_found" {
		t.Errorf("%d %s", recorder.Code, recorder.Body)
	}
}

// The queue answers 200 for either filter.
func TestProblemReportQueuesAnswer(t *testing.T) {
	t.Parallel()
	handler := adminDBHandler(t)
	for _, path := range []string{"/admin/problem-reports", "/admin/problem-reports?status=resolved"} {
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
