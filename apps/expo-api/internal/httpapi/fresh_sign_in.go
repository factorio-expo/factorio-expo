package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/fault"
)

// Fresh sign-in: changing how an account signs in (linking a provider,
// adding a passkey, removing either) needs a sign-in ceremony completed on
// this session within sessions.FreshSignIn. Otherwise the API answers 403
// with the code reauth_required, and the account page asks the person to
// confirm it's them: a passkey (POST /api/v1/me/reauth/passkey/begin and
// finish) or a provider sign-in (GET /api/v1/auth/{provider}/start?reauth=1)
// that must resolve to the same account. Either refreshes the session's
// auth_time, and the page retries. The data export (account_export.go) uses
// it too, and so does account deletion (account_delete.go).

// reauthRequiredCode is the error code for a stale sign-in: the 403 body's
// code, and the ?error= a stale provider link returns to /account with.
const reauthRequiredCode = "reauth_required"

// passkeyReauth is the passkey flow kind of "confirm it's you".
const passkeyReauth = "passkey-reauth"

// requireFreshSignIn refuses a session whose last sign-in ceremony is older
// than sessions.FreshSignIn. It runs inside requireAuth.
func (s *server) requireFreshSignIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r.Context())
		if !ok {
			httpError(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if !session.Fresh(time.Now()) {
			writeCodedError(w, http.StatusForbidden, reauthRequiredCode,
				"confirm it's you: log in again to make this change")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// beginPasskeyReauth starts a sign-in with one of the caller's own passkeys.
func (s *server) beginPasskeyReauth(ctx context.Context, _ *struct{}) (*passkeyRequestOutput, error) {
	session, _ := sessionFromContext(ctx)
	user, err := s.accounts.PasskeyUser(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if len(user.Credentials) == 0 {
		return nil, fault.New(fault.Conflict, "conflict", "this account has no passkey")
	}
	assertion, data, err := s.passkeys.BeginReauth(user)
	if err != nil {
		slog.Error("begin passkey reauth", "error", err)
		return nil, huma.Error500InternalServerError("failed to start the passkey check")
	}
	flow := passkeyFlow{UserID: user.ID, Session: *data}
	cookie, err := s.flowCookie(passkeyFlowCookie, passkeyReauth, flow, passkeyFlowTTL)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to start the passkey check")
	}
	out := &passkeyRequestOutput{Body: assertion.Response}
	out.Add(cookie)
	return out, nil
}

// finishPasskeyReauth verifies the passkey and refreshes the session's
// sign-in time.
func (s *server) finishPasskeyReauth(ctx context.Context, in *passkeyAssertionInput) (*cookiesOutput, error) {
	flow, spent, err := s.takePasskeyFlow(ctx, passkeyReauth)
	if err != nil {
		return nil, err
	}
	session, _ := sessionFromContext(ctx)
	if flow.UserID != session.UserID {
		return nil, withCookieErr(fault.New(fault.Conflict, "conflict", "the passkey check was started by another account"), spent)
	}
	user, err := s.accounts.PasskeyUser(ctx, session.UserID)
	if err != nil {
		return nil, withCookieErr(err, spent)
	}
	credential, err := s.passkeys.FinishReauth(user, flow.Session, in.RawBody)
	if err != nil {
		slog.Info("passkey reauth refused", "error", err)
		return nil, withCookieErr(fault.New(fault.Forbidden, "forbidden", "that passkey is not one of this account's"), spent)
	}
	if err := s.accounts.PasskeySignedIn(ctx, user.ID, credential); err != nil {
		slog.Error("store passkey sign-in", "error", err)
	}
	if err := s.sessions.Confirm(ctx, session); err != nil {
		slog.Error("confirm session", "error", err)
		return nil, withCookieErr(huma.Error500InternalServerError("failed to confirm the session"), spent)
	}
	out := &cookiesOutput{}
	out.Add(spent)
	return out, nil
}

// finishProviderReauth ends a provider "confirm it's you": the identity must
// sign in to the account that started it, which must still be the browser's
// session. A different account (or none) is refused.
func (s *server) finishProviderReauth(ctx context.Context, flow oauthFlow, provider, providerUserID string, spent *http.Cookie) *Redirect {
	session, ok := sessionFromContext(ctx)
	if !ok || session.UserID != flow.ReauthUserID {
		return s.siteRedirect("/account", url.Values{"error": {"failed"}}, spent)
	}
	user, err := s.accounts.FindByIdentity(ctx, provider, providerUserID)
	switch {
	case err == nil && user.ID == session.UserID:
	case err == nil, errors.Is(err, accounts.ErrNotFound):
		return s.siteRedirect("/account", url.Values{"error": {"reauth-other-account"}}, spent)
	default:
		slog.Error("provider reauth lookup", "provider", provider, "error", err)
		return s.siteRedirect("/account", url.Values{"error": {"failed"}}, spent)
	}
	if err := s.sessions.Confirm(ctx, session); err != nil {
		slog.Error("confirm session", "error", err)
		return s.siteRedirect("/account", url.Values{"error": {"failed"}}, spent)
	}
	return s.siteRedirect("/account", url.Values{"reauth": {"done"}}, spent)
}

func (s *server) registerReauth(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "beginPasskeyReauth", Method: http.MethodPost, Path: "/me/reauth/passkey/begin",
		Tags:        []string{"Account"},
		Summary:     "Start confirming it's you with a passkey",
		Description: "Answers the WebAuthn request options for one of the caller's own passkeys.",
		Errors:      []int{http.StatusConflict},
	}, accessRequired, limitSignIn), s.beginPasskeyReauth)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "finishPasskeyReauth", Method: http.MethodPost, Path: "/me/reauth/passkey/finish",
		Tags:         []string{"Account"},
		Summary:      "Finish confirming it's you with a passkey",
		Description:  "Refreshes the session's sign-in time, so the changes that need a fresh sign-in go through.",
		MaxBodyBytes: maxPasskeyBodyBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusConflict},
	}, accessRequired, limitSignIn), s.finishPasskeyReauth)
}
