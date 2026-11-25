package httpapi

import "net/http"

// withSecurityHeaders sets headers every API response carries. JSON is never
// meant to be rendered or framed; responses default to private so shared
// caches (CDN, reverse proxy) never store per-user data. Handlers that serve
// public, cacheable bytes (preview images) override Cache-Control.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Cross-Origin-Resource-Policy", "same-site")
		header.Set("Cache-Control", "private, no-cache")
		next.ServeHTTP(w, r)
	})
}

// withOriginCheck is the CSRF guard for cookie sessions. Browsers always
// label state-changing requests with Origin and/or Sec-Fetch-Site; any write
// coming from a page outside the allowlist is refused before it reaches a
// handler. Non-browser clients send neither header and authenticate with a
// bearer token, which a cross-site page cannot attach.
func (s *server) withOriginCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			httpError(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
			httpError(w, "origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
