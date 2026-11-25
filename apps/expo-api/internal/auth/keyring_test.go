package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	oldSecret = "old-secret-that-is-long-enough-for-hs256"
	newSecret = "new-secret-that-is-long-enough-for-hs256"
)

func tokenKid(t *testing.T, token string) string {
	t.Helper()
	parsed, _, err := jwt.NewParser().ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	kid, _ := parsed.Header["kid"].(string)
	return kid
}

func TestTokenIssuerRotation(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	old, err := NewTokenIssuer(oldSecret, "").Issue("s1", expires)
	if err != nil {
		t.Fatal(err)
	}
	rotated := NewTokenIssuer(newSecret, oldSecret)
	if claims, err := rotated.Verify(old); err != nil || claims.SessionID != "s1" {
		t.Fatalf("token signed with the previous secret: %v", err)
	}
	fresh, err := rotated.Issue("s2", expires)
	if err != nil {
		t.Fatal(err)
	}
	if tokenKid(t, fresh) != keyID(newSecret) || tokenKid(t, fresh) == tokenKid(t, old) {
		t.Error("new tokens must carry the current key id")
	}
	if _, err := rotated.Verify(fresh); err != nil {
		t.Errorf("token signed with the current secret: %v", err)
	}
	// After the previous secret is removed its tokens stop verifying.
	if _, err := NewTokenIssuer(newSecret, "").Verify(old); err == nil {
		t.Error("a token with an unknown key id was accepted")
	}
	// The old key cannot verify tokens of the new one.
	if _, err := NewTokenIssuer(oldSecret, "").Verify(fresh); err == nil {
		t.Error("a token with an unknown key id was accepted")
	}
}

func TestTokenIssuerRequiresKeyID(t *testing.T) {
	noKid, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		SessionID: "s1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString([]byte(newSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewTokenIssuer(newSecret, "").Verify(noKid); err == nil {
		t.Error("a token without a key id was accepted")
	}
}

func TestFlowSignerRotation(t *testing.T) {
	type payload struct{ State string }
	token, err := NewFlowSigner(oldSecret, "").Sign("oauth", payload{State: "abc"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	rotated := NewFlowSigner(newSecret, oldSecret)
	var out payload
	if err := rotated.Open("oauth", token, &out); err != nil || out.State != "abc" {
		t.Fatalf("flow cookie signed with the previous key: %v, %+v", err, out)
	}
	fresh, _ := rotated.Sign("oauth", payload{State: "x"}, time.Minute)
	if tokenKid(t, fresh) != keyID(newSecret) {
		t.Error("new flow tokens must carry the current key id")
	}
	if NewFlowSigner(newSecret, "").Open("oauth", token, &out) == nil {
		t.Error("a flow token with an unknown key id was accepted")
	}
}
