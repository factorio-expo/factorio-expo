package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/settings"
)

// Provider sign-in: GET /api/v1/auth/{provider}/start sends the browser to the
// provider, GET /api/v1/auth/{provider}/callback comes back. A known identity
// signs in; an unknown one goes to the site's /signup page to pick a
// username (auth_signup.go); with ?link=1 a signed-in account adds the
// identity instead (a fresh sign-in, fresh_sign_in.go), and with ?reauth=1
// a signed-in person confirms it's them. Failures go back to the site with
// ?error=.

const (
	oauthFlowTTL  = 10 * time.Minute
	signupFlowTTL = 10 * time.Minute
)

// oauthFlow is the oauth flow cookie: one provider sign-in in progress.
type oauthFlow struct {
	Provider string `json:"provider"`
	State    string `json:"state"`
	// Session is the provider's own session: the PKCE verifier (OAuth) or
	// the return_to URL (Steam).
	Session string `json:"session"`
	Next    string `json:"next"`
	// LinkUserID is set when a signed-in account is adding this provider.
	LinkUserID string `json:"link_user_id,omitempty"`
	// ReauthUserID is set when a signed-in account is confirming it's
	// them: the identity must belong to this account.
	ReauthUserID string `json:"reauth_user_id,omitempty"`
}

// pendingSignup is the signup flow cookie: an identity no account has yet.
type pendingSignup struct {
	Provider       string `json:"provider"`
	ProviderUserID string `json:"provider_user_id"`
	Hint           string `json:"hint"`
	Next           string `json:"next"`
}

type providerResponse struct {
	ID    string `json:"id" doc:"The provider's name, as in the start and callback paths."`
	Label string `json:"label" doc:"The provider's name for people."`
}

type providerList struct {
	Providers []providerResponse `json:"providers" doc:"The enabled sign-in providers."`
}

type providerListOutput struct {
	Body providerList
}

// listProviders names the enabled sign-in providers, for the login page.
func (s *server) listProviders(context.Context, *struct{}) (*providerListOutput, error) {
	providers := []providerResponse{}
	for _, p := range s.identities.List() {
		providers = append(providers, providerResponse{ID: p.Name, Label: p.Label})
	}
	return &providerListOutput{Body: providerList{Providers: providers}}, nil
}

type providerStartInput struct {
	Provider string `path:"provider" doc:"The sign-in provider's name (GET /api/v1/auth/providers)."`
	Next     string `query:"next" doc:"A path on the site to return to after signing in."`
	Link     string `query:"link" enum:"0,1" doc:"1: a signed-in account adds the provider instead of signing in (needs a fresh sign-in)."`
	Reauth   string `query:"reauth" enum:"0,1" doc:"1: a signed-in account confirms it is them with the provider."`
}

func (s *server) startProviderSignIn(ctx context.Context, in *providerStartInput) (*Redirect, error) {
	provider, ok := s.identities.Get(in.Provider)
	if !ok {
		return nil, fault.New(fault.NotFound, "not_found", "unknown sign-in provider")
	}
	// A new sign-in abandons any pending sign-up.
	abandoned := s.expiredFlowCookie(signupFlowCookie)
	flow := oauthFlow{Provider: provider.Name, State: randomToken(), Next: safeNext(in.Next)}
	if in.Link == "1" || in.Reauth == "1" {
		session, signedIn := sessionFromContext(ctx)
		if !signedIn {
			return s.siteRedirect("/login", url.Values{"next": {"/account"}}, abandoned), nil
		}
		flow.Next = "/account"
		switch {
		case in.Reauth == "1":
			flow.ReauthUserID = session.UserID
		case !session.Fresh(time.Now()):
			return s.siteRedirect("/account", url.Values{"error": {reauthRequiredCode}}, abandoned), nil
		default:
			flow.LinkUserID = session.UserID
		}
	}
	authURL, providerSession, err := provider.Begin(flow.State)
	if err != nil {
		slog.Error("start provider sign-in", "provider", provider.Name, "error", err)
		return s.siteRedirect(failurePage(flow), url.Values{"error": {"failed"}}, abandoned), nil
	}
	flow.Session = providerSession
	cookie, err := s.flowCookie(oauthFlowCookie, "oauth", flow, oauthFlowTTL)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to start sign-in")
	}
	out := &Redirect{Location: authURL}
	out.Add(abandoned, cookie)
	return out, nil
}

type providerCallbackInput struct {
	Provider string `path:"provider" doc:"The sign-in provider's name."`
	State    string `query:"state" doc:"The state the start request sent to the provider."`
	Code     string `query:"code" doc:"The provider's authorization code (OAuth providers)."`
	Error    string `query:"error" doc:"The provider's error, when the person declined."`
}

func (s *server) finishProviderSignIn(ctx context.Context, in *providerCallbackInput) (*Redirect, error) {
	r := requestFrom(ctx)
	// Providers send their own parameters too (Steam's openid.*): the
	// provider reads them from the full query.
	query := r.URL.Query()
	var flow oauthFlow
	// A request that does not match the flow leaves its cookie alone (it
	// expires on its own), so a forged callback cannot cancel a real sign-in.
	if !s.readFlowCookie(r, oauthFlowCookie, "oauth", &flow) || flow.Provider != in.Provider ||
		subtle.ConstantTimeCompare([]byte(in.State), []byte(flow.State)) != 1 {
		return s.siteRedirect("/login", url.Values{"error": {"expired"}}), nil
	}
	spent := s.expiredFlowCookie(oauthFlowCookie)
	provider, ok := s.identities.Get(flow.Provider)
	if !ok {
		return s.siteRedirect(failurePage(flow), url.Values{"error": {"failed"}}, spent), nil
	}
	id, err := provider.Complete(ctx, flow.Session, query)
	if err != nil {
		code := "failed"
		if query.Get("error") != "" || query.Get("openid.mode") == "cancel" {
			code = "cancelled"
		} else {
			slog.Warn("provider sign-in failed", "provider", flow.Provider, "error", err)
		}
		return s.siteRedirect(failurePage(flow), url.Values{"error": {code}}, spent), nil
	}

	if flow.LinkUserID != "" {
		return s.finishLink(ctx, flow, id.Provider, id.UserID, spent), nil
	}
	if flow.ReauthUserID != "" {
		return s.finishProviderReauth(ctx, flow, id.Provider, id.UserID, spent), nil
	}
	user, err := s.accounts.FindByIdentity(ctx, id.Provider, id.UserID)
	switch {
	case err == nil:
		cookie, err := s.sessionCookieFor(ctx, user)
		if err != nil {
			return nil, err
		}
		return s.siteRedirect(flow.Next, nil, spent, cookie), nil
	case errors.Is(err, accounts.ErrNotFound):
		// A new account starts here; sign-ups staff paused do not (a sign-up
		// already under way, with its pending cookie, still finishes).
		if !s.catalog.Settings(ctx).Bool(settings.AllowSignups) {
			return s.siteRedirect("/login", url.Values{"error": {"signups-paused"}}, spent), nil
		}
		pending := pendingSignup{Provider: id.Provider, ProviderUserID: id.UserID, Hint: id.Hint, Next: flow.Next}
		cookie, err := s.flowCookie(signupFlowCookie, "signup", pending, signupFlowTTL)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to start sign-up")
		}
		return s.siteRedirect("/signup", nil, spent, cookie), nil
	case errors.Is(err, accounts.ErrIdentityHeld):
		return s.siteRedirect("/login", url.Values{"error": {"identity-held"}}, spent), nil
	default:
		slog.Error("provider sign-in lookup", "provider", id.Provider, "error", err)
		return s.siteRedirect("/login", url.Values{"error": {"failed"}}, spent), nil
	}
}

// finishLink adds the identity to the account that started the flow, which
// must still be the browser's session.
func (s *server) finishLink(ctx context.Context, flow oauthFlow, provider, providerUserID string, spent *http.Cookie) *Redirect {
	if current := viewerFrom(ctx); current != flow.LinkUserID {
		return s.siteRedirect("/account", url.Values{"error": {"failed"}}, spent)
	}
	err := s.accounts.LinkIdentity(ctx, flow.LinkUserID, provider, providerUserID)
	switch {
	case err == nil:
		return s.siteRedirect("/account", url.Values{"linked": {provider}}, spent)
	case errors.Is(err, accounts.ErrIdentityTaken):
		return s.siteRedirect("/account", url.Values{"error": {"identity-taken"}}, spent)
	case errors.Is(err, accounts.ErrProviderLinked):
		return s.siteRedirect("/account", url.Values{"error": {"provider-linked"}}, spent)
	case errors.Is(err, accounts.ErrIdentityHeld):
		return s.siteRedirect("/account", url.Values{"error": {"identity-held"}}, spent)
	default:
		slog.Error("link provider", "provider", provider, "error", err)
		return s.siteRedirect("/account", url.Values{"error": {"failed"}}, spent)
	}
}

// failurePage is where a failed flow returns: the account page for linking
// and confirming, the login page otherwise.
func failurePage(flow oauthFlow) string {
	if flow.LinkUserID != "" || flow.ReauthUserID != "" {
		return "/account"
	}
	return "/login"
}

func (s *server) registerProviders(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listAuthProviders", Method: http.MethodGet, Path: "/auth/providers",
		Tags:    []string{"Sign-in"},
		Summary: "The enabled sign-in providers",
	}, accessPublic, limitRead), s.listProviders)
	huma.Register(api, s.operation(redirectOperation(huma.Operation{
		OperationID: "startProviderSignIn", Method: http.MethodGet, Path: "/auth/{provider}/start",
		Tags:    []string{"Sign-in"},
		Summary: "Send the browser to a provider to sign in",
		Description: "Answers 302 to the provider and sets the sign-in flow cookie. With link=1 a signed-in " +
			"account adds the provider (a fresh sign-in is needed), with reauth=1 it confirms it is them. " +
			"Failures redirect back to the site with an error parameter.",
		Errors: []int{http.StatusNotFound},
	}, "To the provider's sign-in page, or back to the site on a failure."), accessOptional, limitSignIn), s.startProviderSignIn)
	huma.Register(api, s.operation(redirectOperation(huma.Operation{
		OperationID: "finishProviderSignIn", Method: http.MethodGet, Path: "/auth/{provider}/callback",
		Tags:    []string{"Sign-in"},
		Summary: "The provider's callback",
		Description: "Where the provider sends the browser back: it signs in, starts a sign-up or links " +
			"the identity, then redirects to the site. Providers add their own query parameters, so " +
			"parameters are not validated here.",
		SkipValidateParams: true,
	}, "Back to the site, with the session or sign-up cookie set."), accessOptional, limitSignIn), s.finishProviderSignIn)
}
