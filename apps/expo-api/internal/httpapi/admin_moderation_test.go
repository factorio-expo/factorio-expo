package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/staff"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// moderationRequest makes a staff request with a JSON body, as expo-admin
// does, against the staff handler without a database.
func moderationRequest(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testStaffToken)
	request.Header.Set(staff.UserHeader, "alice")
	recorder := httptest.NewRecorder()
	adminTestHandler(t).ServeHTTP(recorder, request)
	return recorder
}

// adminDBHandler is the staff handler wired to TEST_DATABASE_URL, for the
// queue and rejection paths that read or check the catalog.
func adminDBHandler(t *testing.T) http.Handler {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	authenticator, err := staff.NewAuthenticator(testStaffToken)
	if err != nil {
		t.Fatal(err)
	}
	catalogService := catalog.New(pool, catalog.Options{
		Progress: progress.NewHub(), BanHolds: banhold.New("test-ip-secret"),
	})
	holds := banhold.New("test-ip-secret")
	return NewAdmin(AdminDependencies{
		Staff: authenticator, Pool: pool, Catalog: catalogService,
		Ops: staffops.New(catalogService, accountdeletion.New(catalogService, holds)),
	})
}

// staffProblem decodes a problem+json answer.
func staffProblem(t *testing.T, recorder *httptest.ResponseRecorder) Problem {
	t.Helper()
	var problem Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("body %q: %v", recorder.Body, err)
	}
	return problem
}

// A body of the wrong type fails validation before the handler runs, with
// the field named.
func TestModerationBodiesValidateSchema(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, path, body, location string
	}{
		{"resolve entry reports", "/admin/reports/entries/abcDEF123456/resolve", `{"reason":5}`, "body.reason"},
		{"ban entry reporter", "/admin/entry-reports/abcDEF123456/ban-reporter", `{"days":"x"}`, "body.days"},
		{"delete entry", "/admin/entries/abcDEF123456/delete", `{"reason":5}`, "body.reason"},
		{"resolve comment reports", "/admin/reports/comments/abcDEF123456/resolve", `{"reason":[]}`, "body.reason"},
		{"remove comment", "/admin/comments/abcDEF123456/remove", `{"reason":5}`, "body.reason"},
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

// The delete needs a reason: the domain answers its own code when it is
// missing (no schema failure).
func TestDeleteEntryNeedsReason(t *testing.T) {
	t.Parallel()
	handler := adminDBHandler(t)
	request := httptest.NewRequest(http.MethodPost, "/admin/entries/abcDEF123456/delete", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testStaffToken)
	request.Header.Set(staff.UserHeader, "alice")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if problem := staffProblem(t, recorder); recorder.Code != http.StatusBadRequest || problem.Code != "invalid_request" {
		t.Errorf("%d %s", recorder.Code, recorder.Body)
	}
}

// The queues answer 200 with their typed bodies, empty or not.
func TestModerationQueuesAnswer(t *testing.T) {
	t.Parallel()
	handler := adminDBHandler(t)
	for _, path := range []string{"/admin/badges", "/admin/reports/entries", "/admin/reports/comments"} {
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
