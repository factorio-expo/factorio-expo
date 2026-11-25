package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"factorio-expo/apps/expo-api/internal/sessions"
)

type contextKey string

const sessionContextKey contextKey = "session"

// authenticate checks a session token against its server-side session
// (one query per request). ok is false for a token that is forged, expired
// or revoked; err is a database failure.
func (s *server) authenticate(ctx context.Context, token string) (session sessions.Session, ok bool, err error) {
	session, err = s.sessions.Authenticate(ctx, token)
	if errors.Is(err, sessions.ErrNotFound) {
		return sessions.Session{}, false, nil
	}
	if err != nil {
		slog.Error("authenticate session", "error", err)
		return sessions.Session{}, false, err
	}
	return session, true, nil
}

func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential, found := s.requestCredential(r)
		if !found {
			httpError(w, "authentication required", http.StatusUnauthorized)
			return
		}
		session, ok, err := s.authenticate(r.Context(), credential.token)
		if err != nil {
			httpError(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			if credential.fromCookie {
				s.clearSessionCookie(w)
			}
			httpError(w, "invalid or expired session", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey, session)))
	})
}

// sessionFromContext is the caller's session, set by requireAuth or
// withOptionalAuth.
func sessionFromContext(ctx context.Context) (sessions.Session, bool) {
	session, ok := ctx.Value(sessionContextKey).(sessions.Session)
	return session, ok
}

func userIDFromContext(ctx context.Context) (string, bool) {
	session, ok := sessionFromContext(ctx)
	return session.UserID, ok
}

// withOptionalAuth attaches the caller's identity when a valid credential is
// present but still serves anonymous requests, because uploading, reading
// and editing anonymous entries never requires an account. An invalid bearer
// token is rejected; a stale cookie is dropped and the request continues
// anonymously so public pages keep working after a session expires.
func (s *server) withOptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		credential, found := s.requestCredential(r)
		if !found {
			next.ServeHTTP(w, r)
			return
		}
		session, ok, err := s.authenticate(r.Context(), credential.token)
		if err != nil {
			httpError(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !ok {
			if credential.fromCookie {
				s.clearSessionCookie(w)
				next.ServeHTTP(w, r)
				return
			}
			httpError(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey, session)))
	})
}
