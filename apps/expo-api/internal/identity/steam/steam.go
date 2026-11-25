// Package steam is Steam's OpenID 2.0 sign-in (Steam offers ordinary sites
// no OAuth 2.0 or OpenID Connect). It checks the assertion strictly and
// talks HTTPS only.
//
// Flow: Begin sends the browser to Steam with return_to set to our callback
// (carrying the caller's state); Complete checks the assertion's fields,
// asks Steam to verify its signature (check_authentication, assertion.go)
// and reads the persona name with the Web API key. The key is required: the
// provider is enabled only when API_STEAM_API_KEY is set.
package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
)

const (
	defaultLoginURL   = "https://steamcommunity.com/openid/login"
	defaultSummaryURL = "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v0002/"
	openIDNS          = "http://specs.openid.net/auth/2.0"
	identifierSelect  = "http://specs.openid.net/auth/2.0/identifier_select"
	maxResponseBytes  = 64 << 10
)

// claimedIDPattern is a Steam OpenID identity; the group is the SteamID64.
var claimedIDPattern = regexp.MustCompile(`^https?://steamcommunity\.com/openid/id/([0-9]{17})$`)

// Provider is the Steam sign-in (an identity.Method).
type Provider struct {
	// apiKey is the Steam Web API key, used only to read the persona name.
	// It travels in the request URL, so that URL is never logged.
	apiKey      string
	callbackURL string
	client      *http.Client
	loginURL    string
	summaryURL  string
}

// New builds the Steam provider.
func New(apiKey, callbackURL string, client *http.Client) *Provider {
	return &Provider{
		apiKey:      apiKey,
		callbackURL: callbackURL,
		client:      client,
		loginURL:    defaultLoginURL,
		summaryURL:  defaultSummaryURL,
	}
}

// Begin builds Steam's login URL. Steam echoes return_to back verbatim, so
// the state rides in it and the callback sees it as a query parameter. The
// session is return_to itself: the assertion must name exactly it.
func (p *Provider) Begin(state string) (string, string, error) {
	callback, err := url.Parse(p.callbackURL)
	if err != nil {
		return "", "", fmt.Errorf("steam callback URL: %w", err)
	}
	query := callback.Query()
	query.Set("state", state)
	callback.RawQuery = query.Encode()
	returnTo := callback.String()
	realm := callback.Scheme + "://" + callback.Host

	params := url.Values{
		"openid.ns":         {openIDNS},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {realm},
		"openid.identity":   {identifierSelect},
		"openid.claimed_id": {identifierSelect},
	}
	return p.loginURL + "?" + params.Encode(), returnTo, nil
}

// Complete checks Steam's assertion against return_to (the session Begin
// returned) and returns the SteamID and the persona name. A failed persona
// lookup only loses the username suggestion.
func (p *Provider) Complete(ctx context.Context, returnTo string, params url.Values) (string, string, error) {
	steamID, err := p.checkAssertion(ctx, returnTo, params)
	if err != nil {
		return "", "", err
	}
	name, err := p.personaName(ctx, steamID)
	if err != nil {
		slog.Warn("steam persona name lookup failed", "error", err)
	}
	return steamID, name, nil
}

// personaName reads the player summary. Its errors never carry the request
// URL, which holds the API key: a failed request (*url.Error names the URL)
// reports only its cause.
func (p *Provider) personaName(ctx context.Context, steamID string) (string, error) {
	query := url.Values{"key": {p.apiKey}, "steamids": {steamID}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.summaryURL+"?"+query.Encode(), nil)
	if err != nil {
		return "", errors.New("player summary request could not be built")
	}
	resp, err := p.client.Do(request)
	if err != nil {
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			err = urlErr.Err
		}
		return "", fmt.Errorf("player summary request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("player summary answered %d", resp.StatusCode)
	}
	var body struct {
		Response struct {
			Players []struct {
				SteamID     string `json:"steamid"`
				PersonaName string `json:"personaname"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&body); err != nil {
		return "", err
	}
	for _, player := range body.Response.Players {
		if player.SteamID == steamID {
			return player.PersonaName, nil
		}
	}
	return "", errors.New("player summary has no such player")
}
