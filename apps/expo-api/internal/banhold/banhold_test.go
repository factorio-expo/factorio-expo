package banhold_test

import (
	"bytes"
	"testing"

	"factorio-expo/apps/expo-api/internal/banhold"
)

func TestHashes(t *testing.T) {
	h := banhold.New("secret-one")
	if !bytes.Equal(h.Username("Bad_Guy"), h.Username("bad-guy")) || !bytes.Equal(h.Username("BADGUY"), h.Username("badguy")) {
		t.Error("usernames that differ only in case, '_' or '-' must hash alike")
	}
	if bytes.Equal(h.Username("alice"), h.Username("alicia")) {
		t.Error("different usernames hash alike")
	}
	if len(h.Identity("github", "42")) != 32 {
		t.Error("hash is not 32 bytes")
	}
	// Part boundaries count, and kinds never collide.
	if bytes.Equal(h.Identity("github", "42"), h.Identity("github4", "2")) {
		t.Error("identity parts are not separated")
	}
	if bytes.Equal(h.Identity("github", "42"), h.Username("github42")) {
		t.Error("an identity hashes like a username")
	}
	// Another secret gives other hashes.
	if bytes.Equal(h.Username("alice"), banhold.New("secret-two").Username("alice")) {
		t.Error("the key does not change the hash")
	}
}
