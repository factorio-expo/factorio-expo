package httpapi

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCORSAllowsAstroDevOriginAndPatchPreflight(t *testing.T) {
	handler := (&server{
		allowedOrigin: "http://localhost:3000,http://localhost:4321",
	}).withCORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/collections/collection-id", nil)
	request.Header.Set("Origin", "http://localhost:4321")
	request.Header.Set("Access-Control-Request-Method", "PATCH")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:4321" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want localhost:4321", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("Access-Control-Allow-Methods is empty")
	} else if !containsMethod(got, "PATCH") {
		t.Fatalf("Access-Control-Allow-Methods = %q, want PATCH", got)
	}
}

func TestCORSDoesNotAllowUnknownOrigin(t *testing.T) {
	handler := (&server{allowedOrigin: "http://localhost:4321"}).withCORS(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/entries", nil)
	request.Header.Set("Origin", "http://malicious.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected Access-Control-Allow-Origin %q", got)
	}
}

func containsMethod(methods, want string) bool {
	return slices.Contains(splitMethods(methods), want)
}

func splitMethods(methods string) []string {
	var result []string
	for method := range strings.SplitSeq(methods, ",") {
		result = append(result, strings.TrimSpace(method))
	}
	return result
}

func TestRawMaxAgeEndsAtExpiry(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	soon := now.Add(90 * time.Minute)
	past := now.Add(-time.Second)
	cases := []struct {
		expiresAt *time.Time
		want      int
	}{
		{nil, 86400},
		{&soon, 5400},
		{&past, 0},
	}
	for _, c := range cases {
		if got := rawMaxAge(24*time.Hour, c.expiresAt, now); got != c.want {
			t.Errorf("rawMaxAge(%v) = %d, want %d", c.expiresAt, got, c.want)
		}
	}
}
