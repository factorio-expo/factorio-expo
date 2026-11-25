// Package identity signs people in through external providers (GitHub,
// Discord, Google, Steam). Every provider is a Method: GitHub, Discord and
// Google are OAuth 2.0 with PKCE (oauth.go, one file each for the user-info
// call), Steam is OpenID 2.0 (steam/). Adding one is one entry in builders.
// The HTTP layer owns the ceremony state (internal/httpapi/auth_oauth.go);
// this package only starts a provider's sign-in and turns its callback into
// an Identity.
package identity

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Identity is who a provider says signed in.
type Identity struct {
	// Provider is the provider's name (github, discord, google, steam).
	Provider string
	// UserID is the provider's own stable id for the account; accounts are
	// matched by it, never by email or name.
	UserID string
	// Hint is the provider's login or display name, the source of the
	// username suggested at sign-up.
	Hint string
}

// ErrFailed is returned when a provider sign-in did not complete (denied,
// cancelled, or the provider's answer failed verification).
var ErrFailed = errors.New("provider sign-in failed")

// Method is how one provider signs people in.
type Method interface {
	// Begin starts a sign-in carrying state: the URL to send the browser to
	// and the session to keep (in the signed flow cookie) until the callback
	// (OAuth: the PKCE verifier; Steam: the return_to URL).
	Begin(state string) (authURL, session string, err error)
	// Complete verifies the callback parameters against the session Begin
	// returned and fetches who signed in: the provider's stable account id
	// and a name hint (may be empty). The caller has checked state.
	Complete(ctx context.Context, session string, params url.Values) (userID, hint string, err error)
}

// Provider is one enabled sign-in provider.
type Provider struct {
	Name   string
	Label  string
	method Method
}

// Begin starts a sign-in carrying state: the URL to send the browser to and
// the provider session to keep (in the flow cookie) until the callback.
func (p *Provider) Begin(state string) (authURL string, session string, err error) {
	authURL, session, err = p.method.Begin(state)
	if err != nil {
		return "", "", fmt.Errorf("begin %s sign-in: %w", p.Name, err)
	}
	return authURL, session, nil
}

// Complete verifies the provider's callback parameters against the session
// Begin returned and fetches who signed in. The caller has checked state.
func (p *Provider) Complete(ctx context.Context, session string, params url.Values) (Identity, error) {
	userID, hint, err := p.method.Complete(ctx, session, params)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %s: %v", ErrFailed, p.Name, err)
	}
	if userID == "" || len(userID) > 255 {
		return Identity{}, fmt.Errorf("%w: %s returned no usable user id", ErrFailed, p.Name)
	}
	return Identity{Provider: p.Name, UserID: userID, Hint: strings.TrimSpace(hint)}, nil
}

// Registry is the set of enabled providers, in display order.
type Registry struct {
	list   []*Provider
	byName map[string]*Provider
}

// Load enables every provider whose credentials getenv returns (unset
// credentials leave a provider off). callbackBase is the API's public base
// URL; a provider's callback is {callbackBase}/api/v1/auth/{name}/callback.
// client makes the calls to the providers (give it a timeout).
func Load(callbackBase string, getenv func(string) string, client *http.Client) (*Registry, error) {
	base, err := url.Parse(strings.TrimRight(callbackBase, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("API public URL %q is not an http(s) URL", callbackBase)
	}
	registry := &Registry{byName: map[string]*Provider{}}
	for _, b := range builders {
		creds, enabled, err := b.credentials(getenv)
		if err != nil {
			return nil, err
		}
		if !enabled {
			continue
		}
		callback := base.String() + "/api/v1/auth/" + b.name + "/callback"
		provider := &Provider{Name: b.name, Label: b.label, method: b.build(creds, callback, client)}
		registry.list = append(registry.list, provider)
		registry.byName[b.name] = provider
	}
	return registry, nil
}

// List returns the enabled providers in display order.
func (r *Registry) List() []*Provider { return r.list }

// Get returns an enabled provider by name.
func (r *Registry) Get(name string) (*Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// Label is a provider's display name, or the name itself for one that is
// no longer enabled.
func (r *Registry) Label(name string) string {
	if p, ok := r.byName[name]; ok {
		return p.Label
	}
	if b, ok := builderByName(name); ok {
		return b.label
	}
	return name
}
