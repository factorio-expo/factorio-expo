package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"factorio-expo/apps/expo-api/internal/accounts"
	"factorio-expo/apps/expo-api/internal/fault"
)

// The account page's sign-in methods: linked providers and passkeys. Linking
// a provider is GET /api/v1/auth/{provider}/start?link=1 (auth_oauth.go);
// adding a passkey is the passkey-add ceremony (auth_passkeys.go). Every
// change needs a fresh sign-in (fresh_sign_in.go); a removal also signs the
// account out everywhere but this session.

type identityResponse struct {
	accounts.IdentityMethod
	Label string `json:"label" doc:"The provider's name for people."`
}

type signInMethodsResponse struct {
	Identities []identityResponse       `json:"identities" doc:"The linked providers."`
	Passkeys   []accounts.PasskeyMethod `json:"passkeys" doc:"The account's passkeys."`
	Providers  []providerResponse       `json:"providers" doc:"The enabled providers, to link another."`
}

type signInMethodsOutput struct {
	Body signInMethodsResponse
}

func (s *server) listSignInMethods(ctx context.Context, _ *struct{}) (*signInMethodsOutput, error) {
	methods, err := s.accounts.ListMethods(ctx, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	response := signInMethodsResponse{
		Identities: []identityResponse{},
		Passkeys:   methods.Passkeys,
		Providers:  []providerResponse{},
	}
	for _, identity := range methods.Identities {
		response.Identities = append(response.Identities, identityResponse{
			IdentityMethod: identity, Label: s.identities.Label(identity.Provider),
		})
	}
	for _, p := range s.identities.List() {
		response.Providers = append(response.Providers, providerResponse{ID: p.Name, Label: p.Label})
	}
	return &signInMethodsOutput{Body: response}, nil
}

type removeIdentityInput struct {
	Provider string `path:"provider" doc:"The linked provider to remove."`
}

func (s *server) removeIdentity(ctx context.Context, in *removeIdentityInput) (*struct{}, error) {
	session, _ := sessionFromContext(ctx)
	return nil, s.accounts.RemoveIdentity(ctx, session.UserID, in.Provider, session.ID)
}

type removePasskeyInput struct {
	PasskeyID string `path:"passkeyID" doc:"The passkey's id."`
}

func (s *server) removePasskey(ctx context.Context, in *removePasskeyInput) (*struct{}, error) {
	session, _ := sessionFromContext(ctx)
	passkeyID, err := parseID(in.PasskeyID)
	if err != nil {
		return nil, fault.New(fault.NotFound, "not_found", "not found")
	}
	return nil, s.accounts.RemovePasskey(ctx, session.UserID, passkeyID, session.ID)
}

type newRecoveryCodeResponse struct {
	RecoveryCode string `json:"recovery_code" doc:"The account's new recovery code, shown once; the previous code no longer works."`
}

type newRecoveryCodeOutput struct {
	Body newRecoveryCodeResponse
}

func (s *server) newRecoveryCode(ctx context.Context, _ *struct{}) (*newRecoveryCodeOutput, error) {
	code, err := s.accounts.RegenerateRecoveryCode(ctx, viewerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return &newRecoveryCodeOutput{Body: newRecoveryCodeResponse{RecoveryCode: code}}, nil
}

func (s *server) registerSignInMethods(api huma.API) {
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "newRecoveryCode", Method: http.MethodPost, Path: "/me/recovery-code",
		Tags:    []string{"Account"},
		Summary: "Generate a new recovery code",
		Description: "Needs a fresh sign-in. Replaces the account's recovery code: " +
			"the old code stops working at once. The response is the only place the new code " +
			"is ever shown. Other sessions stay signed in.",
		Errors: []int{http.StatusNotFound},
	}, accessFresh, limitWrite), s.newRecoveryCode)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "listSignInMethods", Method: http.MethodGet, Path: "/me/sign-in-methods",
		Tags:    []string{"Account"},
		Summary: "The account's linked providers and passkeys",
	}, accessRequired, limitRead), s.listSignInMethods)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "removeIdentity", Method: http.MethodDelete, Path: "/me/identities/{provider}",
		Tags:        []string{"Account"},
		Summary:     "Unlink a provider",
		Description: "Needs a fresh sign-in; also signs the account out everywhere but this session.",
		Errors:      []int{http.StatusConflict, http.StatusNotFound},
	}, accessFresh, limitWrite), s.removeIdentity)
	huma.Register(api, s.operation(huma.Operation{
		OperationID: "removePasskey", Method: http.MethodDelete, Path: "/me/passkeys/{passkeyID}",
		Tags:        []string{"Account"},
		Summary:     "Remove a passkey",
		Description: "Needs a fresh sign-in; also signs the account out everywhere but this session.",
		Errors:      []int{http.StatusConflict, http.StatusNotFound},
	}, accessFresh, limitWrite), s.removePasskey)
}
