package httpapi

import (
	"bytes"
	"testing"
	"time"
)

func tokensAt(secret string, now *time.Time) *importTokens {
	tokens := newImportTokens([]byte(secret))
	tokens.now = func() time.Time { return *now }
	return tokens
}

func TestImportTokenBindsSourceStringAndTime(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	tokens := tokensAt("a-long-enough-test-secret-value-1234", &now)
	const source, exchange = "https://pastebin.com/aBcD1234", "0eNqrVkrKKU0tKMrMTS1WsqqOrQUAJxkFKQ=="
	token := tokens.sign(source, exchange)

	if !tokens.valid(token, source, exchange) {
		t.Fatal("a fresh token for its own source and string is refused")
	}
	for name, check := range map[string]bool{
		"another source": tokens.valid(token, "https://pastebin.com/zZzZ9999", exchange),
		"another string": tokens.valid(token, source, exchange+"A"),
		"empty":          tokens.valid("", source, exchange),
		"not base64":     tokens.valid("!!!", source, exchange),
		"truncated":      tokens.valid(token[:len(token)-4], source, exchange),
		"extended":       tokens.valid(token+"AAAA", source, exchange),
	} {
		if check {
			t.Errorf("%s: accepted", name)
		}
	}

	// A token from another secret, and one with its expiry stretched, fail.
	other := tokensAt("a-different-long-test-secret-value-5678", &now)
	if other.valid(token, source, exchange) {
		t.Error("a token signed with another secret was accepted")
	}
	raw := []byte(token)
	forged := string(bytes.Replace(raw, raw[:2], []byte("zz"), 1))
	if forged != token && tokens.valid(forged, source, exchange) {
		t.Error("a token with an altered expiry was accepted")
	}

	// Good for the lifetime, then not.
	now = now.Add(importTokenLifetime - time.Second)
	if !tokens.valid(token, source, exchange) {
		t.Error("expired early")
	}
	now = now.Add(2 * time.Second)
	if tokens.valid(token, source, exchange) {
		t.Error("still valid past the lifetime")
	}
}

func TestImportTokenKeyIsDerivedNotTheRawSecret(t *testing.T) {
	secret := []byte("a-long-enough-test-secret-value-1234")
	tokens := newImportTokens(secret)
	if bytes.Equal(tokens.key, secret) || len(tokens.key) != 32 {
		t.Fatal("the signing key is the raw secret or the wrong size")
	}
	// The IP-hash key is the raw secret: a token's MAC must not equal an IP hash of anything.
	if again := newImportTokens(secret); !bytes.Equal(again.key, tokens.key) {
		t.Fatal("derivation is not deterministic")
	}
}
