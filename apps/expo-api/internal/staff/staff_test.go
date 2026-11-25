package staff

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testToken = strings.Repeat("t", MinTokenLength)

func request(token, username string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/admin/badges", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if username != "" {
		r.Header.Set(UserHeader, username)
	}
	return r
}

func TestNewAuthenticatorRefusesShortTokens(t *testing.T) {
	if _, err := NewAuthenticator(testToken[1:]); err == nil {
		t.Fatal("a short token was accepted")
	}
}

func TestAuthenticate(t *testing.T) {
	a, err := NewAuthenticator(testToken)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Authenticate(request(testToken, " bob "))
	if err != nil || got != (Identity{Username: "bob"}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, tc := range []struct {
		name, token, username string
		want                  error
	}{
		{"no token", "", "bob", ErrBadToken},
		{"wrong token", strings.Repeat("x", MinTokenLength), "bob", ErrBadToken},
		{"token prefix", testToken[:10], "bob", ErrBadToken},
		{"token with more", testToken + "x", "bob", ErrBadToken},
		{"no username", testToken, "", ErrNoUser},
		{"blank username", testToken, "   ", ErrNoUser},
		{"control character", testToken, "b\x01ob", ErrNoUser},
		{"too long", testToken, strings.Repeat("b", 65), ErrNoUser},
	} {
		if _, err := a.Authenticate(request(tc.token, tc.username)); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
}
