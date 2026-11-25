// Package turnstile checks Cloudflare Turnstile tokens server-side with
// siteverify. The site's sign-up forms run the widget; the API verifies the
// token it produced once, with the action the form names and the site's own
// host name.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const siteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// maxTokenLength is the longest token Turnstile issues.
const maxTokenLength = 2048

var (
	// ErrFailed: the token is missing, invalid, used, expired, or was made
	// for another action or host name. The person may try again.
	ErrFailed = errors.New("turnstile: check not passed")
	// ErrUnavailable: siteverify could not answer (network, timeout, an
	// error on Cloudflare's side, or a wrong secret key).
	ErrUnavailable = errors.New("turnstile: siteverify unavailable")
)

// testSecrets are Cloudflare's dummy secret keys
// (developers.cloudflare.com/turnstile/troubleshooting/testing/): they always
// pass, always fail, or report a spent token, accept only the dummy token the
// test site keys produce, and answer with host name "example.com" and no
// action. With one of them the action and host name are not checked, so the
// forms work on localhost.
var testSecrets = []string{
	"1x0000000000000000000000000000000AA",
	"2x0000000000000000000000000000000AA",
	"3x0000000000000000000000000000000AA",
}

// IsTestSecret reports whether secret is one of Cloudflare's dummy keys.
func IsTestSecret(secret string) bool { return slices.Contains(testSecrets, secret) }

// serverErrors are siteverify error codes that are not the visitor's fault.
var serverErrors = []string{"missing-input-secret", "invalid-input-secret", "bad-request", "internal-error"}

// Verifier calls siteverify with the widget's secret key.
type Verifier struct {
	secret   string
	hostname string
	client   *http.Client
}

// New builds a verifier for the widget whose secret key is secret, accepting
// only tokens solved on hostname (the site's own). client should carry a
// short timeout: sign-up waits for it.
func New(secret, hostname string, client *http.Client) *Verifier {
	return &Verifier{secret: secret, hostname: hostname, client: client}
}

type siteverifyResponse struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
}

// Verify checks token, which a widget running action produced for the
// visitor at remoteIP (optional). It returns nil, ErrFailed or
// ErrUnavailable, wrapped with the detail.
func (v *Verifier) Verify(ctx context.Context, token, action, remoteIP string) error {
	if token == "" || len(token) > maxTokenLength {
		return fmt.Errorf("%w: no usable token", ErrFailed)
	}
	form := url.Values{"secret": {v.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, siteverifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := v.client.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrUnavailable, response.StatusCode)
	}
	var result siteverifyResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrUnavailable, err)
	}
	if !result.Success {
		codes := strings.Join(result.ErrorCodes, ",")
		if slices.ContainsFunc(result.ErrorCodes, func(code string) bool { return slices.Contains(serverErrors, code) }) {
			return fmt.Errorf("%w: %s", ErrUnavailable, codes)
		}
		return fmt.Errorf("%w: %s", ErrFailed, codes)
	}
	if IsTestSecret(v.secret) {
		return nil
	}
	if result.Action != action {
		return fmt.Errorf("%w: action %q, want %q", ErrFailed, result.Action, action)
	}
	if !strings.EqualFold(result.Hostname, v.hostname) {
		return fmt.Errorf("%w: host name %q, want %q", ErrFailed, result.Hostname, v.hostname)
	}
	return nil
}
