package steam

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const steamID = "76561197960287930"

// fakeSteam answers check_authentication with verdict and the player summary
// with a persona name. It records the API key it was sent.
func fakeSteam(t *testing.T, verdict string) (*Provider, *string) {
	t.Helper()
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/openid/login":
			if err := r.ParseForm(); err != nil || r.PostForm.Get("openid.mode") != "check_authentication" {
				http.Error(w, "bad verification request", http.StatusBadRequest)
				return
			}
			fmt.Fprint(w, verdict)
		case "/summary":
			gotKey = r.URL.Query().Get("key")
			fmt.Fprintf(w, `{"response":{"players":[{"steamid":%q,"personaname":"Factorio Fan"}]}}`, steamID)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	p := New("web-api-key", "https://expo.test/api/v1/auth/steam/callback", server.Client())
	p.loginURL = server.URL + "/openid/login"
	p.summaryURL = server.URL + "/summary"
	return p, &gotKey
}

// assertion is what Steam appends to return_to after a successful sign-in.
func assertion(p *Provider, returnTo string) url.Values {
	claimed := "https://steamcommunity.com/openid/id/" + steamID
	return url.Values{
		"openid.ns":             {openIDNS},
		"openid.mode":           {"id_res"},
		"openid.op_endpoint":    {p.loginURL},
		"openid.claimed_id":     {claimed},
		"openid.identity":       {claimed},
		"openid.return_to":      {returnTo},
		"openid.response_nonce": {"2026-11-03T10:00:00Zabc"},
		"openid.assoc_handle":   {"1234567890"},
		"openid.signed":         {"signed,op_endpoint,claimed_id,identity,return_to,response_nonce,assoc_handle"},
		"openid.sig":            {"c2lnbmF0dXJl"},
	}
}

// begin starts a sign-in and returns the auth URL and return_to (the
// session).
func begin(t *testing.T, p *Provider) (authURL, returnTo string) {
	t.Helper()
	authURL, returnTo, err := p.Begin("state-123")
	if err != nil {
		t.Fatal(err)
	}
	return authURL, returnTo
}

func TestBeginCarriesState(t *testing.T) {
	p, _ := fakeSteam(t, "")
	authURL, returnTo := begin(t, p)
	if returnTo != "https://expo.test/api/v1/auth/steam/callback?state=state-123" {
		t.Fatalf("return_to = %q", returnTo)
	}
	parsed, _ := url.Parse(authURL)
	query := parsed.Query()
	if got := query.Get("openid.realm"); got != "https://expo.test" {
		t.Errorf("realm = %q", got)
	}
	if got := query.Get("openid.return_to"); got != returnTo {
		t.Errorf("auth URL return_to = %q", got)
	}
	if got := query.Get("openid.mode"); got != "checkid_setup" {
		t.Errorf("mode = %q", got)
	}
}

func TestComplete(t *testing.T) {
	p, gotKey := fakeSteam(t, "ns:"+openIDNS+"\nis_valid:true\n")
	_, returnTo := begin(t, p)
	id, name, err := p.Complete(context.Background(), returnTo, assertion(p, returnTo))
	if err != nil {
		t.Fatal(err)
	}
	if id != steamID || name != "Factorio Fan" {
		t.Errorf("Complete = %q, %q", id, name)
	}
	if *gotKey != "web-api-key" {
		t.Errorf("summary key = %q", *gotKey)
	}
}

func TestCompleteRejects(t *testing.T) {
	valid := "ns:" + openIDNS + "\nis_valid:true\n"
	cases := map[string]struct {
		verdict string
		edit    func(url.Values)
	}{
		"steam says invalid":    {"ns:" + openIDNS + "\nis_valid:false\n", nil},
		"steam answers nothing": {"", nil},
		"truncated answer":      {"ns:" + openIDNS, nil},
		"cancelled":             {valid, func(v url.Values) { v.Set("openid.mode", "cancel") }},
		"other return_to": {valid, func(v url.Values) {
			v.Set("openid.return_to", "https://evil.test/api/v1/auth/steam/callback?state=state-123")
		}},
		"other endpoint": {valid, func(v url.Values) { v.Set("openid.op_endpoint", "https://evil.test/openid/login") }},
		"lookalike host": {valid, func(v url.Values) {
			id := "https://steamcommunityXcom/openid/id/" + steamID
			v.Set("openid.claimed_id", id)
			v.Set("openid.identity", id)
		}},
		"identity differs": {valid, func(v url.Values) {
			v.Set("openid.identity", "https://steamcommunity.com/openid/id/76561197960287931")
		}},
		"claimed_id unsigned": {valid, func(v url.Values) {
			v.Set("openid.signed", strings.Replace(v.Get("openid.signed"), "claimed_id,", "", 1))
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, _ := fakeSteam(t, c.verdict)
			_, returnTo := begin(t, p)
			params := assertion(p, returnTo)
			if c.edit != nil {
				c.edit(params)
			}
			if id, _, err := p.Complete(context.Background(), returnTo, params); err == nil || id != "" {
				t.Fatalf("assertion accepted: %q", id)
			}
		})
	}
}

// A flow without a return_to cannot match an assertion without one.
func TestCompleteNeedsSession(t *testing.T) {
	p, _ := fakeSteam(t, "ns:"+openIDNS+"\nis_valid:true\n")
	params := assertion(p, "")
	params.Del("openid.return_to")
	if _, _, err := p.Complete(context.Background(), "", params); err == nil {
		t.Fatal("assertion accepted without a session")
	}
}

// The player summary URL carries the API key; a failed lookup must not put
// it in the error or the log.
func TestPersonaLookupFailureHidesAPIKey(t *testing.T) {
	p, _ := fakeSteam(t, "ns:"+openIDNS+"\nis_valid:true\n")
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	p.summaryURL = closed.URL + "/summary"

	if _, err := p.personaName(context.Background(), steamID); err == nil || strings.Contains(err.Error(), "web-api-key") {
		t.Fatalf("persona lookup error = %v, want one without the key", err)
	}

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	_, returnTo := begin(t, p)
	id, name, err := p.Complete(context.Background(), returnTo, assertion(p, returnTo))
	if err != nil || id != steamID || name != "" {
		t.Fatalf("Complete = %q, %q, %v; want the SteamID without a name", id, name, err)
	}
	if !strings.Contains(logs.String(), "persona name lookup failed") || strings.Contains(logs.String(), "web-api-key") {
		t.Errorf("log = %q, want the failure without the key", logs.String())
	}
}
