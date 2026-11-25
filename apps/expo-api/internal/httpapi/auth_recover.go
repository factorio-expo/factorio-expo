package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// Account recovery: the username and the recovery code shown at sign-up
// (internal/accounts/recoverycode.go) sign in an account that lost every
// sign-in method. The code is used up and replaced, and the new one comes
// back in the response for the site to show. The session is a fresh sign-in
// (fresh_sign_in.go), so the person can add a provider or a passkey at once.

type recoverRequest struct {
	Username     string `json:"username" minLength:"1" maxLength:"64" doc:"The account's username, in any case."`
	RecoveryCode string `json:"recovery_code" minLength:"1" maxLength:"64" doc:"The recovery code, with or without its dashes, in any case."`
}

type recoverInput struct {
	Body recoverRequest
}

func (s *server) recoverAccount(ctx context.Context, in *recoverInput) (*signedInOutput, error) {
	user, code, err := s.accounts.Recover(ctx, strings.TrimSpace(in.Body.Username), in.Body.RecoveryCode)
	if err != nil {
		return nil, err
	}
	// A new sign-in abandons any pending provider sign-up.
	out, err := s.signedIn(ctx, user, "", s.expiredFlowCookie(signupFlowCookie))
	if err != nil {
		return nil, err
	}
	slog.Info("account recovered", "user", user.ID)
	out.Body.RecoveryCode = code
	return out, nil
}

func (s *server) registerRecover(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "recoverAccount", Method: http.MethodPost, Path: "/auth/recover",
		Tags:    []string{"Sign-in"},
		Summary: "Sign in with a recovery code",
		Description: "For an account that lost every sign-in method. A matching username and recovery code " +
			"sign the account in (sets the session cookie, a fresh sign-in) and use the code up: the " +
			"response carries its replacement. Every mismatch (no such username, a wrong code, an account " +
			"without one, a deleted account) answers the same 401 recovery_failed.",
		MaxBodyBytes: 4 << 10,
		Errors:       []int{http.StatusBadRequest, http.StatusUnauthorized},
	}, accessPublic, limitSignIn), s.recoverAccount)
}
