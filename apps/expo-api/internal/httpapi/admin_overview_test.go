package httpapi

import (
	"net/http"
	"testing"
)

// The Overview is a staff route: it refuses a request without the token
// before it reads the database.
func TestOverviewRefusesMissingToken(t *testing.T) {
	t.Parallel()
	if got := adminRequest(t, http.MethodGet, "/admin/overview", "", "alice").Code; got != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", got)
	}
}
