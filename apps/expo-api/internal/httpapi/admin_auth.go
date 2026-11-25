package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"factorio-expo/apps/expo-api/internal/staff"
)

const staffContextKey contextKey = "staff"

// requireStaff checks the shared token and staff username on every request
// and puts the staff identity in the context. A missing or wrong token is
// 401; a right token without a valid username is 400.
func (s *adminServer) requireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := s.staff.Authenticate(r)
		if errors.Is(err, staff.ErrNoUser) {
			httpError(w, "staff username required", http.StatusBadRequest)
			return
		}
		if err != nil {
			slog.Warn("staff request rejected", "path", r.URL.Path, "remote_addr", r.RemoteAddr, "error", err)
			httpError(w, "authentication required", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), staffContextKey, identity)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func staffFromContext(ctx context.Context) (staff.Identity, bool) {
	identity, ok := ctx.Value(staffContextKey).(staff.Identity)
	return identity, ok
}
