package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"
)

// Flow cookies carry a sign-in ceremony's state from its first request to
// its second (OAuth state and provider session, a pending sign-up, a
// WebAuthn challenge), signed by auth.FlowSigner. HttpOnly and SameSite=Lax:
// a provider's callback is a top-level GET navigation, which Lax allows.
const (
	oauthFlowCookie   = "expo_oauth"
	signupFlowCookie  = "expo_signup"
	passkeyFlowCookie = "expo_passkey"
)

func (s *server) flowCookieName(name string) string {
	if s.cookieSecure {
		return "__Host-" + name
	}
	return name
}

// flowCookie signs data as kind and wraps it for ttl.
func (s *server) flowCookie(name, kind string, data any, ttl time.Duration) (*http.Cookie, error) {
	token, err := s.flows.Sign(kind, data, ttl)
	if err != nil {
		return nil, err
	}
	return &http.Cookie{
		Name:     s.flowCookieName(name),
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// readFlowCookie decodes a flow cookie of kind into out.
func (s *server) readFlowCookie(r *http.Request, name, kind string, out any) bool {
	cookie, err := r.Cookie(s.flowCookieName(name))
	if err != nil || cookie.Value == "" {
		return false
	}
	return s.flows.Open(kind, cookie.Value, out) == nil
}

// expiredFlowCookie removes a flow cookie: a ceremony's state is used once.
func (s *server) expiredFlowCookie(name string) *http.Cookie {
	return &http.Cookie{
		Name:     s.flowCookieName(name),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// randomToken is an unguessable value for OAuth state.
func randomToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
