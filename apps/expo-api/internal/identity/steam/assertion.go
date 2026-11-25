package steam

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// signedFields must all be covered by Steam's signature, or an attacker
// could swap an unsigned one (another account's claimed_id, say).
var signedFields = []string{"op_endpoint", "claimed_id", "identity", "return_to", "response_nonce", "assoc_handle"}

// checkAssertion checks Steam's positive assertion and has Steam verify its
// signature; it returns the SteamID64. Only an answer that says exactly
// "is_valid:true" passes.
func (p *Provider) checkAssertion(ctx context.Context, returnTo string, params url.Values) (string, error) {
	if mode := params.Get("openid.mode"); mode != "id_res" {
		return "", fmt.Errorf("steam sign-in not completed (mode %q)", mode)
	}
	if params.Get("openid.ns") != openIDNS {
		return "", errors.New("steam assertion has the wrong namespace")
	}
	if params.Get("openid.op_endpoint") != p.loginURL {
		return "", errors.New("steam assertion names another endpoint")
	}
	if returnTo == "" || params.Get("openid.return_to") != returnTo {
		return "", errors.New("steam assertion is for another return URL")
	}
	claimed := params.Get("openid.claimed_id")
	match := claimedIDPattern.FindStringSubmatch(claimed)
	if match == nil || params.Get("openid.identity") != claimed {
		return "", errors.New("steam assertion has no valid Steam identity")
	}
	signed := strings.Split(params.Get("openid.signed"), ",")
	for _, field := range signedFields {
		if !slices.Contains(signed, field) {
			return "", fmt.Errorf("steam assertion does not sign %s", field)
		}
	}
	if err := p.verify(ctx, params, signed); err != nil {
		return "", err
	}
	return match[1], nil
}

// verify asks Steam whether it issued this assertion (OpenID 2.0
// check_authentication). Steam answers each assertion once, so a replayed
// callback fails here too.
func (p *Provider) verify(ctx context.Context, params url.Values, signed []string) error {
	form := url.Values{
		"openid.ns":           {openIDNS},
		"openid.mode":         {"check_authentication"},
		"openid.sig":          {params.Get("openid.sig")},
		"openid.signed":       {params.Get("openid.signed")},
		"openid.assoc_handle": {params.Get("openid.assoc_handle")},
	}
	for _, field := range signed {
		form.Set("openid."+field, params.Get("openid."+field))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("steam verification: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("steam verification: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("steam verification answered %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("steam verification: %w", err)
	}
	fields := map[string]string{}
	for line := range strings.SplitSeq(string(body), "\n") {
		if key, value, ok := strings.Cut(strings.TrimRight(line, "\r"), ":"); ok {
			fields[key] = value
		}
	}
	if fields["ns"] != openIDNS || fields["is_valid"] != "true" {
		return errors.New("steam did not verify the assertion")
	}
	return nil
}
