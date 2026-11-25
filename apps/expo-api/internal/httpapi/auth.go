package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/sessions"
	"factorio-expo/internal/shortid"
)

// Accounts have no password: they sign in with a provider (auth_oauth.go,
// auth_signup.go) or a passkey (auth_passkeys.go). Every sign-in ends in
// signedIn or sessionCookieFor, which start a server-side session
// (internal/sessions) and put its JWT in the session cookie.

// authResponse is what a sign-in returns. The token itself is never in the
// body: it is set as an HttpOnly cookie so page scripts cannot read it.
type authResponse struct {
	UserID   string `json:"user_id" doc:"The account's id."`
	Username string `json:"username" doc:"The account's username."`
	// Next is where the browser goes after a provider sign-up.
	Next string `json:"next,omitempty" doc:"Where the browser goes next (a path on the site); provider sign-ups only."`
	// RecoveryCode is the account's new recovery code, in the one response
	// that carries it.
	RecoveryCode string `json:"recovery_code,omitempty" doc:"The account's recovery code, shown once: set by a sign-up and by a recovery (which replaces the code it used). The site shows it to the person to keep."`
}

type userResponse struct {
	UserID   string `json:"user_id" doc:"The account's id."`
	Username string `json:"username" doc:"The account's username."`
}

// signedInOutput is a sign-in: the session cookie and the account.
type signedInOutput struct {
	SetCookies
	Body authResponse
}

// cookiesOutput is a response that only sets or clears cookies (204).
type cookiesOutput struct {
	SetCookies
}

type userOutput struct {
	Body userResponse
}

// logout signs this device out: it deletes the caller's session, so a
// copied token stops working too, and clears the cookie. It needs no valid
// session, so a stale cookie can always be removed. A pending provider
// sign-up is dropped too. Signing out everywhere is DELETE /api/v1/me/sessions.
func (s *server) logout(ctx context.Context, _ *struct{}) (*cookiesOutput, error) {
	out := &cookiesOutput{}
	out.Add(s.expiredSessionCookie(), s.expiredFlowCookie(signupFlowCookie))
	if credential, found := s.requestCredential(requestFrom(ctx)); found {
		session, ok, err := s.authenticate(ctx, credential.token)
		if err == nil && ok {
			err = s.sessions.End(ctx, session.UserID, session.ID)
		}
		if err != nil && !errors.Is(err, sessions.ErrNotFound) {
			slog.Error("end session on logout", "error", err)
			return nil, withCookies(huma.Error500InternalServerError("failed to end the session"), out.SetCookies)
		}
	}
	return out, nil
}

func (s *server) me(ctx context.Context, _ *struct{}) (*userOutput, error) {
	user, err := s.queries.GetUserByID(ctx, viewerFrom(ctx))
	if err != nil {
		return nil, fault.New(fault.NotFound, "not_found", "user not found")
	}
	return &userOutput{Body: userResponse{UserID: user.ID, Username: user.Username}}, nil
}

// sessionCookieFor starts a session for a freshly authenticated user and
// returns the cookie that carries its token.
func (s *server) sessionCookieFor(ctx context.Context, user accounts.User) (*http.Cookie, error) {
	token, err := s.sessions.Start(ctx, user.ID)
	if err != nil {
		slog.Error("start session", "error", err)
		return nil, huma.Error500InternalServerError("failed to start the session")
	}
	return s.sessionCookie(token, s.sessions.TTL()), nil
}

// signedIn starts the session and answers with the account, for the
// sign-ins the browser makes with fetch; extra cookies are set too.
func (s *server) signedIn(ctx context.Context, user accounts.User, next string, extra ...*http.Cookie) (*signedInOutput, error) {
	cookie, err := s.sessionCookieFor(ctx, user)
	if err != nil {
		return nil, err
	}
	out := &signedInOutput{Body: authResponse{UserID: user.ID, Username: user.Username, Next: next}}
	out.Add(cookie)
	out.Add(extra...)
	return out, nil
}

// safeNext keeps a post-sign-in destination on the site: a path, never
// another host ("//evil", "/\evil") or a scheme.
func safeNext(raw string) string {
	if raw == "" || len(raw) > 1024 || !strings.HasPrefix(raw, "/") ||
		strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, `/\`) ||
		strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "/"
	}
	return raw
}

// siteLocation is the address of a page of the site (the API and the site
// differ in local development), with optional query parameters.
func (s *server) siteLocation(path string, query url.Values) string {
	target := s.siteURL + path
	if len(query) > 0 {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		target += separator + query.Encode()
	}
	return target
}

// siteRedirect sends the browser to a page of the site, setting cookies on
// the way.
func (s *server) siteRedirect(path string, query url.Values, cookies ...*http.Cookie) *Redirect {
	out := &Redirect{Location: s.siteLocation(path, query)}
	out.Add(cookies...)
	return out
}

// parseID rejects a malformed id from a path.
func parseID(value string) (string, error) {
	if !shortid.Valid(value) {
		return "", errors.New("malformed id")
	}
	return value, nil
}

func (s *server) registerAuth(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "logout", Method: http.MethodPost, Path: "/auth/logout",
		Tags:    []string{"Account"},
		Summary: "Sign this device out",
		Description: "Needs no valid session: it always clears the session cookie, and ends the session " +
			"the request's cookie or bearer token names. Signing out everywhere is DELETE /api/v1/me/sessions.",
		Errors: []int{http.StatusInternalServerError},
	}, accessPublic, limitNone), s.logout)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getMe", Method: http.MethodGet, Path: "/me",
		Tags:    []string{"Account"},
		Summary: "The signed-in account",
		Errors:  []int{http.StatusNotFound},
	}, accessRequired, limitRead), s.me)
}
