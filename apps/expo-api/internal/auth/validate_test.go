package auth

import (
	"testing"
	"time"
)

func TestSuggestUsername(t *testing.T) {
	cases := map[string]string{
		"octocat":            "octocat",
		"john.doe":           "john_doe",
		"John Smith":         "John_Smith",
		"  [TAG] Ünïcode!! ": "TAG_ncode",
		"--a--":              "",
		"ab":                 "",
		"日本語":                "",
		"a..b":               "a_b",
		"this-is-a-very-long-github-login-name-39": "this-is-a-very-long-github-login",
		"abcdefghijklmnopqrstuvwxyzabcde_xyz":      "abcdefghijklmnopqrstuvwxyzabcde",
	}
	for name, want := range cases {
		if got := SuggestUsername(name); got != want {
			t.Errorf("SuggestUsername(%q) = %q, want %q", name, got, want)
		}
		if got := SuggestUsername(name); got != "" && ValidateUsername(got) != nil {
			t.Errorf("SuggestUsername(%q) = %q is not a valid username", name, got)
		}
	}
}

func TestFlowSigner(t *testing.T) {
	signer := NewFlowSigner("q3VJ7yXc0n9Rk2s8FvLp4WbTzHa6MdEu1GiYoNxC", "")
	type payload struct{ State string }
	token, err := signer.Sign("oauth", payload{State: "abc"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var out payload
	if err := signer.Open("oauth", token, &out); err != nil || out.State != "abc" {
		t.Fatalf("Open = %v, %+v", err, out)
	}
	if signer.Open("signup", token, &out) == nil {
		t.Error("a token of another kind was accepted")
	}
	other := NewFlowSigner("another-secret-that-is-long-enough-32b", "")
	if other.Open("oauth", token, &out) == nil {
		t.Error("a token signed with another key was accepted")
	}
	expired, _ := signer.Sign("oauth", payload{}, -time.Second)
	if signer.Open("oauth", expired, &out) == nil {
		t.Error("an expired token was accepted")
	}
	// A flow token is never a session token.
	if _, err := NewTokenIssuer("q3VJ7yXc0n9Rk2s8FvLp4WbTzHa6MdEu1GiYoNxC", "").Verify(token); err == nil {
		t.Error("a flow token verified as a session token")
	}
}
