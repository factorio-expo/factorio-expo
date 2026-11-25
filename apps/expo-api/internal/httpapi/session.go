package httpapi

import (
	"net/http"
	"strings"
	"time"
)

// Browser sessions ride in an HttpOnly cookie so page scripts (and therefore
// any XSS) can never read the token. The __Host- prefix pins it to this exact
// host over HTTPS; plain-HTTP local development uses the unprefixed name.
const (
	secureSessionCookie   = "__Host-expo_session"
	insecureSessionCookie = "expo_session"
)

func (s *server) sessionCookieName() string {
	if s.cookieSecure {
		return secureSessionCookie
	}
	return insecureSessionCookie
}

// sessionCookie is the cookie of a freshly issued token. SameSite=Lax keeps
// the cookie off cross-site subrequests and POSTs; withOriginCheck covers the
// rest.
func (s *server) sessionCookie(token string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// expiredSessionCookie removes the session cookie.
func (s *server) expiredSessionCookie() *http.Cookie {
	cookie := s.sessionCookie("", 0)
	cookie.MaxAge = -1
	return cookie
}

// clearSessionCookie is expiredSessionCookie for the auth middleware, which
// answers before any operation output exists.
func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, s.expiredSessionCookie())
}

// credential is where a request's token came from. Bearer is for API clients
// and expo-web SSR (which lifts the browser's cookie); cookie is the browser.
type credential struct {
	token      string
	fromCookie bool
}

func (s *server) requestCredential(r *http.Request) (credential, bool) {
	if token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found {
		return credential{token: token}, token != ""
	}
	cookie, err := r.Cookie(s.sessionCookieName())
	if err != nil || cookie.Value == "" {
		return credential{}, false
	}
	return credential{token: cookie.Value, fromCookie: true}, true
}
