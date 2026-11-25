package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// toServer sends every request to one test server, whatever its host.
type toServer struct{ target *url.URL }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host, r.Host = s.target.Scheme, s.target.Host, s.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// fakeSiteverify answers siteverify for one secret: "good" passes for
// "signup-passkey" on example.org, "other-action" and "other-host"
// pass for the wrong action or host, anything else fails.
func fakeSiteverify(t *testing.T, seen *url.Values) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/turnstile/v0/siteverify" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseForm()
		if seen != nil {
			*seen = r.PostForm
		}
		answer := map[string]any{"success": true, "action": "signup-passkey", "hostname": "example.org"}
		switch {
		case r.PostForm.Get("secret") != "secret":
			answer = map[string]any{"success": false, "error-codes": []string{"invalid-input-secret"}}
		case r.PostForm.Get("response") == "other-action":
			answer["action"] = "signup-provider"
		case r.PostForm.Get("response") == "other-host":
			answer["hostname"] = "evil.example"
		case r.PostForm.Get("response") != "good":
			answer = map[string]any{"success": false, "error-codes": []string{"invalid-input-response"}}
		}
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(server.Close)
	return server
}

func verifierFor(server *httptest.Server, secret string) *Verifier {
	target, _ := url.Parse(server.URL)
	return New(secret, "example.org", &http.Client{Transport: toServer{target: target}, Timeout: time.Second})
}

func TestVerify(t *testing.T) {
	var seen url.Values
	server := fakeSiteverify(t, &seen)
	verifier := verifierFor(server, "secret")
	ctx := context.Background()

	if err := verifier.Verify(ctx, "good", "signup-passkey", "203.0.113.9"); err != nil {
		t.Fatalf("good token: %v", err)
	}
	if seen.Get("secret") != "secret" || seen.Get("response") != "good" || seen.Get("remoteip") != "203.0.113.9" {
		t.Errorf("siteverify got %v", seen)
	}

	cases := []struct {
		name, token string
		want        error
	}{
		{"failed check", "bad", ErrFailed},
		{"wrong action", "other-action", ErrFailed},
		{"wrong host name", "other-host", ErrFailed},
		{"no token", "", ErrFailed},
		{"oversized token", strings.Repeat("x", maxTokenLength+1), ErrFailed},
	}
	for _, c := range cases {
		if err := verifier.Verify(ctx, c.token, "signup-passkey", ""); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}

	if err := verifierFor(server, "wrong-secret").Verify(ctx, "good", "signup-passkey", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("wrong secret: err = %v, want ErrUnavailable", err)
	}
}

func TestVerifyUnavailable(t *testing.T) {
	ctx := context.Background()

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "oops", http.StatusBadGateway)
	}))
	defer broken.Close()
	if err := verifierFor(broken, "secret").Verify(ctx, "good", "signup-passkey", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("502: err = %v, want ErrUnavailable", err)
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(release)
	target, _ := url.Parse(slow.URL)
	impatient := New("secret", "example.org",
		&http.Client{Transport: toServer{target: target}, Timeout: 50 * time.Millisecond})
	if err := impatient.Verify(ctx, "good", "signup-passkey", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("timeout: err = %v, want ErrUnavailable", err)
	}

	gone := httptest.NewServer(http.NotFoundHandler())
	unreachable := verifierFor(gone, "secret")
	gone.Close()
	if err := unreachable.Verify(ctx, "good", "signup-passkey", ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("unreachable: err = %v, want ErrUnavailable", err)
	}
}

// Cloudflare's test secrets answer for host "example.com" with no action, so
// with one of them only success counts: the forms work on localhost.
func TestVerifyWithATestSecret(t *testing.T) {
	t.Parallel()
	const testSecret = "1x0000000000000000000000000000000AA"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		answer := map[string]any{"success": true, "hostname": "example.com"}
		if r.PostForm.Get("response") != "XXXX.DUMMY.TOKEN.XXXX" {
			answer = map[string]any{"success": false, "error-codes": []string{"invalid-input-response"}}
		}
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(server.Close)
	verifier := verifierFor(server, testSecret)
	if err := verifier.Verify(context.Background(), "XXXX.DUMMY.TOKEN.XXXX", "upload", ""); err != nil {
		t.Fatalf("dummy token = %v", err)
	}
	if err := verifier.Verify(context.Background(), "real-token", "upload", ""); !errors.Is(err, ErrFailed) {
		t.Fatalf("a refused token = %v", err)
	}
	if !IsTestSecret(testSecret) || IsTestSecret("secret") {
		t.Fatal("IsTestSecret")
	}
}
