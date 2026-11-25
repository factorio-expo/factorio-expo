package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"golang.org/x/oauth2"
)

// maxUserInfoBytes bounds a provider's user-info answer.
const maxUserInfoBytes = 64 << 10

// oauthProvider is an OAuth 2.0 authorization-code sign-in with PKCE (S256):
// the verifier is the session kept in the flow cookie, so a stolen code is
// useless in another browser's callback. After the exchange one user-info
// call reads the account's stable id and a name hint.
type oauthProvider struct {
	config      oauth2.Config
	client      *http.Client
	userInfoURL string
	// parseUser reads the user-info answer: the stable id and a name hint.
	parseUser func(body []byte) (userID, hint string, err error)
}

// Begin builds the provider's authorization URL with a fresh PKCE verifier.
func (p *oauthProvider) Begin(state string) (string, string, error) {
	verifier := oauth2.GenerateVerifier()
	return p.config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), verifier, nil
}

// Complete exchanges the callback's code with the PKCE verifier and reads
// who signed in.
func (p *oauthProvider) Complete(ctx context.Context, verifier string, params url.Values) (string, string, error) {
	if denied := params.Get("error"); denied != "" {
		return "", "", fmt.Errorf("provider answered %q", denied)
	}
	code := params.Get("code")
	if code == "" || verifier == "" {
		return "", "", errors.New("callback has no code or the flow no verifier")
	}
	token, err := p.config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, p.client), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", "", fmt.Errorf("token exchange: %w", err)
	}
	body, err := p.userInfo(ctx, token)
	if err != nil {
		return "", "", err
	}
	return p.parseUser(body)
}

// userInfo reads the user-info endpoint with the access token.
func (p *oauthProvider) userInfo(ctx context.Context, token *oauth2.Token) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	token.SetAuthHeader(request)
	resp, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("user info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUserInfoBytes))
	if err != nil {
		return nil, fmt.Errorf("user info: %w", err)
	}
	return body, nil
}

// decodeUser decodes a user-info answer into v.
func decodeUser(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("user info: %w", err)
	}
	return nil
}

// secretInBody sends the client credentials as form fields, which all the
// providers document, instead of letting x/oauth2 guess (a failed guess
// repeats the exchange with the other style).
func secretInBody(endpoint oauth2.Endpoint) oauth2.Endpoint {
	endpoint.AuthStyle = oauth2.AuthStyleInParams
	return endpoint
}
