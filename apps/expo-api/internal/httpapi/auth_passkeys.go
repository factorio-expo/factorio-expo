package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/passkey"
	"factorio-expo/internal/shortid"
)

// Passkey ceremonies, each a begin/finish pair: begin answers the WebAuthn
// options (the browser passes them to navigator.credentials) and keeps the
// challenge in the passkey flow cookie; finish takes the browser's
// credential JSON. Sign-in needs no username (discoverable passkeys);
// sign-up asks for one first, because the passkey is stored under it.

const (
	passkeyFlowTTL      = 5 * time.Minute
	maxPasskeyBodyBytes = 64 << 10
)

// passkeyFlow is the passkey flow cookie. Its signing kind names the
// purpose, so a sign-in challenge cannot finish a sign-up.
type passkeyFlow struct {
	UserID   string               `json:"user_id,omitempty"`
	Username string               `json:"username,omitempty"`
	Session  webauthn.SessionData `json:"session"`
	// HumanChecked: a sign-up's begin passed Turnstile, so its finish needs
	// no token of its own.
	HumanChecked bool `json:"human_checked,omitempty"`
}

const (
	passkeyLogin  = "passkey-login"
	passkeySignup = "passkey-signup"
	passkeyAdd    = "passkey-add"
)

// PasskeyCreationOptions are the options navigator.credentials.create takes.
type PasskeyCreationOptions = protocol.PublicKeyCredentialCreationOptions

// PasskeyRequestOptions are the options navigator.credentials.get takes.
type PasskeyRequestOptions = protocol.PublicKeyCredentialRequestOptions

// passkeyCreationOutput answers a begin that registers a passkey.
type passkeyCreationOutput struct {
	SetCookies
	Body PasskeyCreationOptions
}

// passkeyRequestOutput answers a begin that signs in with a passkey.
type passkeyRequestOutput struct {
	SetCookies
	Body PasskeyRequestOptions
}

// PasskeyAttestation is the browser's credential for a registration
// (navigator.credentials.create, as the WebAuthn client's JSON gives it).
type PasskeyAttestation struct {
	ID       string `json:"id" doc:"The credential id, base64url."`
	RawID    string `json:"rawId" doc:"The credential id, base64url."`
	Type     string `json:"type" enum:"public-key" doc:"Always public-key."`
	Response struct {
		ClientDataJSON    string   `json:"clientDataJSON" doc:"The client data, base64url."`
		AttestationObject string   `json:"attestationObject" doc:"The attestation object, base64url."`
		_                 struct{} `additionalProperties:"true"`
	} `json:"response" doc:"The authenticator's answer."`
	_ struct{} `additionalProperties:"true"`
}

// PasskeyAssertion is the browser's credential for a sign-in or a check
// (navigator.credentials.get, as the WebAuthn client's JSON gives it).
type PasskeyAssertion struct {
	ID       string `json:"id" doc:"The credential id, base64url."`
	RawID    string `json:"rawId" doc:"The credential id, base64url."`
	Type     string `json:"type" enum:"public-key" doc:"Always public-key."`
	Response struct {
		ClientDataJSON    string   `json:"clientDataJSON" doc:"The client data, base64url."`
		AuthenticatorData string   `json:"authenticatorData" doc:"The authenticator data, base64url."`
		Signature         string   `json:"signature" doc:"The signature, base64url."`
		UserHandle        string   `json:"userHandle,omitempty" doc:"The user handle, base64url."`
		_                 struct{} `additionalProperties:"true"`
	} `json:"response" doc:"The authenticator's answer."`
	_ struct{} `additionalProperties:"true"`
}

type passkeyAttestationInput struct {
	RawBody []byte
	Body    PasskeyAttestation
}

type passkeyAssertionInput struct {
	RawBody []byte
	Body    PasskeyAssertion
}

func (s *server) beginPasskeyLogin(ctx context.Context, _ *struct{}) (*passkeyRequestOutput, error) {
	assertion, session, err := s.passkeys.BeginLogin()
	if err != nil {
		slog.Error("begin passkey sign-in", "error", err)
		return nil, huma.Error500InternalServerError("failed to start passkey sign-in")
	}
	cookie, err := s.flowCookie(passkeyFlowCookie, passkeyLogin, passkeyFlow{Session: *session}, passkeyFlowTTL)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to start passkey sign-in")
	}
	out := &passkeyRequestOutput{Body: assertion.Response}
	// A new sign-in abandons any pending provider sign-up.
	out.Add(s.expiredFlowCookie(signupFlowCookie), cookie)
	return out, nil
}

// takePasskeyFlow reads the ceremony's flow cookie; whatever follows, the
// cookie is spent.
func (s *server) takePasskeyFlow(ctx context.Context, kind string) (passkeyFlow, *http.Cookie, error) {
	var flow passkeyFlow
	spent := s.expiredFlowCookie(passkeyFlowCookie)
	if !s.readFlowCookie(requestFrom(ctx), passkeyFlowCookie, kind, &flow) {
		return flow, spent, withCookieErr(fault.New(fault.Invalid, "invalid_request", "the passkey request expired, try again"), spent)
	}
	return flow, spent, nil
}

// withCookieErr attaches a cookie to a refusal.
func withCookieErr(err error, cookies ...*http.Cookie) error {
	var jar SetCookies
	jar.Add(cookies...)
	return withCookies(err, jar)
}

func (s *server) finishPasskeyLogin(ctx context.Context, in *passkeyAssertionInput) (*signedInOutput, error) {
	flow, spent, err := s.takePasskeyFlow(ctx, passkeyLogin)
	if err != nil {
		return nil, err
	}
	user, credential, err := s.passkeys.FinishLogin(flow.Session, in.RawBody, func(userID string) (passkey.User, error) {
		return s.accounts.PasskeyUser(ctx, userID)
	})
	if err != nil {
		slog.Info("passkey sign-in refused", "error", err)
		return nil, withCookieErr(fault.New(fault.Unauthorized, "unauthorized", "this passkey is not registered here"), spent)
	}
	if err := s.accounts.PasskeySignedIn(ctx, user.ID, credential); err != nil {
		slog.Error("store passkey sign-in", "error", err)
	}
	account, err := s.queries.GetUserByID(ctx, user.ID)
	if err != nil {
		return nil, withCookieErr(fault.New(fault.Unauthorized, "unauthorized", "this passkey is not registered here"), spent)
	}
	out, err := s.signedIn(ctx, accounts.User{ID: account.ID, Username: account.Username}, "", spent)
	return out, withCookieErr(err, spent)
}

func (s *server) beginPasskeySignup(ctx context.Context, in *signupInput) (*passkeyCreationOutput, error) {
	r := requestFrom(ctx)
	abandoned := s.expiredFlowCookie(signupFlowCookie)
	username := strings.TrimSpace(in.Body.Username)
	if err := s.usernameFree(ctx, username); err != nil {
		return nil, withCookieErr(err, abandoned)
	}
	if err := s.verifyHuman(r, in.Body.TurnstileToken, turnstileActionPasskey); err != nil {
		return nil, withCookieErr(err, abandoned)
	}
	user := passkey.User{ID: shortid.New(), Username: username}
	creation, session, err := s.passkeys.BeginRegistration(user)
	if err != nil {
		slog.Error("begin passkey sign-up", "error", err)
		return nil, withCookieErr(huma.Error500InternalServerError("failed to start passkey sign-up"), abandoned)
	}
	flow := passkeyFlow{
		UserID: user.ID, Username: user.Username, Session: *session, HumanChecked: s.turnstile != nil,
	}
	cookie, err := s.flowCookie(passkeyFlowCookie, passkeySignup, flow, passkeyFlowTTL)
	if err != nil {
		return nil, withCookieErr(huma.Error500InternalServerError("failed to start passkey sign-up"), abandoned)
	}
	out := &passkeyCreationOutput{Body: creation.Response}
	out.Add(abandoned, cookie)
	return out, nil
}

func (s *server) finishPasskeySignup(ctx context.Context, in *passkeyAttestationInput) (*signedInOutput, error) {
	flow, spent, err := s.takePasskeyFlow(ctx, passkeySignup)
	if err != nil {
		return nil, err
	}
	// The begin passed Turnstile (a ceremony begun while it was off cannot
	// finish once it is on).
	if s.turnstile != nil && !flow.HumanChecked {
		return nil, withCookieErr(fault.New(fault.Forbidden, turnstileFailedCode, "the human check did not pass, try again"), spent)
	}
	user := passkey.User{ID: flow.UserID, Username: flow.Username}
	credential, err := s.passkeys.FinishRegistration(user, flow.Session, in.RawBody)
	if err != nil {
		slog.Info("passkey sign-up refused", "error", err)
		return nil, withCookieErr(fault.New(fault.Invalid, "invalid_request", "the passkey could not be verified"), spent)
	}
	account, code, err := s.accounts.CreateWithPasskey(ctx, user.ID, user.Username, credential)
	if err != nil {
		return nil, withCookieErr(err, spent)
	}
	s.chargeRegistration(requestFrom(ctx))
	out, err := s.signedIn(ctx, account, "", spent)
	if err != nil {
		return nil, withCookieErr(err, spent)
	}
	out.Body.RecoveryCode = code
	return out, nil
}

// beginAddPasskey starts adding a passkey to the signed-in account.
func (s *server) beginAddPasskey(ctx context.Context, _ *struct{}) (*passkeyCreationOutput, error) {
	user, err := s.accounts.PasskeyUser(ctx, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	creation, session, err := s.passkeys.BeginRegistration(user)
	if err != nil {
		slog.Error("begin adding a passkey", "error", err)
		return nil, huma.Error500InternalServerError("failed to start adding a passkey")
	}
	flow := passkeyFlow{UserID: user.ID, Session: *session}
	cookie, err := s.flowCookie(passkeyFlowCookie, passkeyAdd, flow, passkeyFlowTTL)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to start adding a passkey")
	}
	out := &passkeyCreationOutput{Body: creation.Response}
	out.Add(cookie)
	return out, nil
}

func (s *server) finishAddPasskey(ctx context.Context, in *passkeyAttestationInput) (*cookiesOutput, error) {
	flow, spent, err := s.takePasskeyFlow(ctx, passkeyAdd)
	if err != nil {
		return nil, err
	}
	userID := viewerFrom(ctx)
	if flow.UserID != userID {
		return nil, withCookieErr(fault.New(fault.Conflict, "conflict", "the passkey was started by another account"), spent)
	}
	user, err := s.accounts.PasskeyUser(ctx, userID)
	if err != nil {
		return nil, withCookieErr(err, spent)
	}
	credential, err := s.passkeys.FinishRegistration(user, flow.Session, in.RawBody)
	if err != nil {
		slog.Info("adding a passkey refused", "error", err)
		return nil, withCookieErr(fault.New(fault.Invalid, "invalid_request", "the passkey could not be verified"), spent)
	}
	if err := s.accounts.AddPasskey(ctx, userID, credential); err != nil {
		return nil, withCookieErr(err, spent)
	}
	out := &cookiesOutput{}
	out.Add(spent)
	return out, nil
}

func (s *server) registerPasskeys(api huma.API) {
	const tag = "Sign-in"
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "beginPasskeyLogin", Method: http.MethodPost, Path: "/auth/passkeys/login/begin",
		Tags:    []string{tag},
		Summary: "Start a passkey sign-in",
		Description: "Answers the WebAuthn request options and keeps the challenge in the passkey flow " +
			"cookie. Needs no username: passkeys are discoverable.",
	}, accessPublic, limitSignIn), s.beginPasskeyLogin)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "finishPasskeyLogin", Method: http.MethodPost, Path: "/auth/passkeys/login/finish",
		Tags:         []string{tag},
		Summary:      "Finish a passkey sign-in",
		Description:  "Takes the browser's credential and sets the session cookie.",
		MaxBodyBytes: maxPasskeyBodyBytes,
		Errors:       []int{http.StatusBadRequest},
	}, accessPublic, limitSignIn), s.finishPasskeyLogin)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "beginPasskeySignup", Method: http.MethodPost, Path: "/auth/passkeys/signup/begin",
		Tags:         []string{tag},
		Summary:      "Start a sign-up with a passkey",
		Description:  "Checks the username, then answers the WebAuthn creation options.",
		MaxBodyBytes: 4 << 10,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict},
	}, accessPublic, limitSignIn, s.registrationGate(), s.signupsGate()), s.beginPasskeySignup)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "finishPasskeySignup", Method: http.MethodPost, Path: "/auth/passkeys/signup/finish",
		Tags:         []string{tag},
		Summary:      "Finish a sign-up with a passkey",
		Description:  "Creates the account and sets the session cookie.",
		MaxBodyBytes: maxPasskeyBodyBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict},
	}, accessPublic, limitSignIn, s.registrationGate()), s.finishPasskeySignup)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "beginAddPasskey", Method: http.MethodPost, Path: "/me/passkeys/begin",
		Tags:        []string{"Account"},
		Summary:     "Start adding a passkey to the account",
		Description: "Needs a fresh sign-in; its flow cookie carries the permission to the finish.",
	}, accessFresh, limitWrite), s.beginAddPasskey)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "finishAddPasskey", Method: http.MethodPost, Path: "/me/passkeys/finish",
		Tags:         []string{"Account"},
		Summary:      "Finish adding a passkey to the account",
		MaxBodyBytes: maxPasskeyBodyBytes,
		Errors:       []int{http.StatusBadRequest, http.StatusConflict},
	}, accessRequired, limitWrite), s.finishAddPasskey)
}
