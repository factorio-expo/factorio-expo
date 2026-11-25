package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/auth"
	"factorio-expo/apps/expo-api/internal/fault"
)

// A provider identity no account has yet waits in the signup flow cookie
// while the site's /signup page asks for a username: GET /api/v1/auth/signup
// tells the page which provider and suggests a username, POST creates the
// account and signs it in.

type pendingSignupResponse struct {
	Provider      string `json:"provider" doc:"The provider the identity came from."`
	ProviderLabel string `json:"provider_label" doc:"The provider's name for people."`
	// Suggestion is the provider's name made into a valid, free username,
	// or empty.
	Suggestion string `json:"suggestion" doc:"The provider's name made into a valid, free username, or empty."`
}

type pendingSignupOutput struct {
	Body pendingSignupResponse
}

// signupRequest is the body of both sign-up forms (a provider's username
// step, the passkey sign-up begin).
type signupRequest struct {
	Username string `json:"username" doc:"The username to create the account under."`
	// TurnstileToken is the form's Turnstile widget token; required when
	// Turnstile is on (signup_guard.go).
	TurnstileToken string `json:"turnstile_token,omitempty" doc:"The form's Turnstile token; required while Turnstile is on."`
}

type signupInput struct {
	Body signupRequest
}

// usernameFree validates a requested username and checks nobody has it.
func (s *server) usernameFree(ctx context.Context, username string) error {
	if err := auth.ValidateUsername(username); err != nil {
		return fault.New(fault.Invalid, "invalid_request", err.Error())
	}
	available, err := s.accounts.UsernameAvailable(ctx, username)
	if err != nil {
		return err
	}
	if !available {
		return accounts.ErrUsernameTaken
	}
	return nil
}

func (s *server) getPendingSignup(ctx context.Context, _ *struct{}) (*pendingSignupOutput, error) {
	var pending pendingSignup
	if !s.readFlowCookie(requestFrom(ctx), signupFlowCookie, "signup", &pending) {
		return nil, fault.New(fault.NotFound, "not_found", "no sign-up in progress")
	}
	suggestion := auth.SuggestUsername(pending.Hint)
	if suggestion != "" {
		available, err := s.accounts.UsernameAvailable(ctx, suggestion)
		if err != nil {
			slog.Error("check username", "error", err)
		}
		if err != nil || !available {
			suggestion = ""
		}
	}
	return &pendingSignupOutput{Body: pendingSignupResponse{
		Provider:      pending.Provider,
		ProviderLabel: s.identities.Label(pending.Provider),
		Suggestion:    suggestion,
	}}, nil
}

func (s *server) completeSignup(ctx context.Context, in *signupInput) (*signedInOutput, error) {
	r := requestFrom(ctx)
	var pending pendingSignup
	if !s.readFlowCookie(r, signupFlowCookie, "signup", &pending) {
		return nil, fault.New(fault.NotFound, "not_found", "no sign-up in progress")
	}
	username := strings.TrimSpace(in.Body.Username)
	if err := s.usernameFree(ctx, username); err != nil {
		return nil, err
	}
	if err := s.verifyHuman(r, in.Body.TurnstileToken, turnstileActionProvider); err != nil {
		return nil, err
	}
	user, code, err := s.accounts.CreateWithIdentity(ctx, username, pending.Provider, pending.ProviderUserID)
	if err != nil {
		return nil, err
	}
	s.chargeRegistration(r)
	out, err := s.signedIn(ctx, user, safeNext(pending.Next), s.expiredFlowCookie(signupFlowCookie))
	if err != nil {
		return nil, err
	}
	out.Body.RecoveryCode = code
	return out, nil
}

func (s *server) registerSignup(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "getPendingSignup", Method: http.MethodGet, Path: "/auth/signup",
		Tags:        []string{"Sign-in"},
		Summary:     "The provider sign-up in progress",
		Description: "Which provider the pending sign-up came from and a suggested username; needs the sign-up flow cookie.",
		Errors:      []int{http.StatusNotFound},
	}, accessPublic, limitSignIn), s.getPendingSignup)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "completeSignup", Method: http.MethodPost, Path: "/auth/signup",
		Tags:         []string{"Sign-in"},
		Summary:      "Create the account of a provider sign-up and sign it in",
		Description:  "Needs the sign-up flow cookie; sets the session cookie.",
		MaxBodyBytes: 4 << 10,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict, http.StatusNotFound},
	}, accessPublic, limitSignIn, s.registrationGate()), s.completeSignup)
}
