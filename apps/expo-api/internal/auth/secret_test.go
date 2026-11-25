package auth

import (
	"os"
	"strings"
	"testing"
)

func TestValidateSecret(t *testing.T) {
	cases := map[string]bool{
		"":                                    false,
		strings.Repeat("x", MinSecretBytes-1): false,
		"Change-Me-but-long-enough-to-pass-32-bytes": false,
		"q3VJ7yXc0n9Rk2s8FvLp4WbTzHa6MdEu1GiYoNxC":   true,
	}
	for secret, ok := range cases {
		if err := ValidateSecret(secret); (err == nil) != ok {
			t.Errorf("ValidateSecret(%q) = %v, want ok=%v", secret, err, ok)
		}
	}
}

// The committed .env.example must never hold a secret the API accepts.
func TestEnvExampleSecretsAreRejected(t *testing.T) {
	body, err := os.ReadFile("../../../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for line := range strings.SplitSeq(string(body), "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (name != "API_JWT_SECRET" && name != "API_IP_HASH_SECRET") {
			continue
		}
		found++
		if ValidateSecret(value) == nil {
			t.Errorf(".env.example %s is accepted as a real secret", name)
		}
	}
	if found != 2 {
		t.Fatalf("found %d secrets in .env.example, want 2", found)
	}
}
