package httpapi

import (
	"net/http"
	"strings"
)

// withCORS answers preflight requests and adds CORS headers for the configured
// browser origins only; unknown origins are served without them.
func (s *server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.originAllowed(r.Header.Get("Origin")) {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			// Credentials let a browser on an allowed origin send the session
			// cookie when the API is on another origin (local development).
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Edit-Token")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// originAllowed matches a request origin against the comma-separated allowlist.
func (s *server) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	for allowed := range strings.SplitSeq(s.allowedOrigin, ",") {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
}
