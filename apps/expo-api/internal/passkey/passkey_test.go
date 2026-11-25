package passkey

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const site = "http://localhost:3000"

var rp = virtualwebauthn.RelyingParty{ID: "localhost", Name: "Factorio Expo", Origin: site}

func newService(t *testing.T) *Service {
	t.Helper()
	s, err := New(site)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func encode(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// register creates a passkey for user on a virtual authenticator.
func register(t *testing.T, s *Service, user User) (virtualwebauthn.Authenticator, virtualwebauthn.Credential, *webauthn.Credential) {
	t.Helper()
	creation, session, err := s.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	options, err := virtualwebauthn.ParseAttestationOptions(encode(t, creation.Response))
	if err != nil {
		t.Fatal(err)
	}
	device := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
		UserHandle: []byte(user.ID),
	})
	key := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	answer := virtualwebauthn.CreateAttestationResponse(rp, device, key, *options)
	credential, err := s.FinishRegistration(user, *session, []byte(answer))
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	device.AddCredential(key)
	return device, key, credential
}

func login(t *testing.T, s *Service, device virtualwebauthn.Authenticator, key virtualwebauthn.Credential) (webauthn.SessionData, []byte) {
	t.Helper()
	assertion, session, err := s.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	options, err := virtualwebauthn.ParseAssertionOptions(encode(t, assertion.Response))
	if err != nil {
		t.Fatal(err)
	}
	return *session, []byte(virtualwebauthn.CreateAssertionResponse(rp, device, key, *options))
}

func TestRegisterThenSignIn(t *testing.T) {
	s := newService(t)
	user := User{ID: "aaaaaaaaaaaa", Username: "alice"}
	device, key, credential := register(t, s, user)
	user.Credentials = []webauthn.Credential{*credential}

	lookup := func(userID string) (User, error) {
		if userID != user.ID {
			return User{}, errors.New("no such user")
		}
		return user, nil
	}
	session, answer := login(t, s, device, key)
	found, _, err := s.FinishLogin(session, answer, lookup)
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if found.ID != user.ID {
		t.Fatalf("signed in as %q, want %q", found.ID, user.ID)
	}

	// The same challenge and answer cannot be used twice.
	if _, _, err := s.FinishLogin(session, answer, lookup); !errors.Is(err, ErrFailed) {
		t.Errorf("replayed sign-in = %v, want ErrFailed", err)
	}
}

func TestSignInRefusesUnknownPasskey(t *testing.T) {
	s := newService(t)
	user := User{ID: "bbbbbbbbbbbb", Username: "bob"}
	device, key, _ := register(t, s, user)
	// The account exists but does not hold this passkey.
	session, answer := login(t, s, device, key)
	_, _, err := s.FinishLogin(session, answer, func(string) (User, error) { return user, nil })
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("unknown passkey = %v, want ErrFailed", err)
	}
}

func TestRegistrationRefusesOtherOrigin(t *testing.T) {
	s := newService(t)
	user := User{ID: "cccccccccccc", Username: "carol"}
	creation, session, err := s.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	options, _ := virtualwebauthn.ParseAttestationOptions(encode(t, creation.Response))
	evil := virtualwebauthn.RelyingParty{ID: "localhost", Name: "Evil", Origin: "http://localhost:6666"}
	answer := virtualwebauthn.CreateAttestationResponse(evil, virtualwebauthn.NewAuthenticator(),
		virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2), *options)
	_, err = s.FinishRegistration(user, *session, []byte(answer))
	if !errors.Is(err, ErrFailed) {
		t.Fatalf("other origin = %v, want ErrFailed", err)
	}
	// The log names the origin the browser sent, the clue to a misconfigured site URL.
	if !strings.Contains(err.Error(), "http://localhost:6666") {
		t.Fatalf("other origin error %q does not name the received origin", err)
	}
}

func TestCeremoniesRequireUserVerification(t *testing.T) {
	s := newService(t)
	user := User{ID: "dddddddddddd", Username: "dave"}

	// The options ask the browser to verify the person.
	creation, session, err := s.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	if got := creation.Response.AuthenticatorSelection.UserVerification; got != protocol.VerificationRequired {
		t.Errorf("registration user verification = %q, want required", got)
	}
	assertion, _, err := s.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	if got := assertion.Response.UserVerification; got != protocol.VerificationRequired {
		t.Errorf("sign-in user verification = %q, want required", got)
	}

	// An authenticator that did not verify the person is refused at
	// registration...
	options, _ := virtualwebauthn.ParseAttestationOptions(encode(t, creation.Response))
	unverified := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{
		UserHandle: []byte(user.ID), UserNotVerified: true,
	})
	answer := virtualwebauthn.CreateAttestationResponse(rp, unverified,
		virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2), *options)
	if _, err := s.FinishRegistration(user, *session, []byte(answer)); !errors.Is(err, ErrFailed) {
		t.Errorf("unverified registration = %v, want ErrFailed", err)
	}

	// ...and at sign-in, even with a registered passkey.
	device, key, credential := register(t, s, user)
	user.Credentials = []webauthn.Credential{*credential}
	device.Options.UserNotVerified = true
	loginSession, loginAnswer := login(t, s, device, key)
	if _, _, err := s.FinishLogin(loginSession, loginAnswer, func(string) (User, error) { return user, nil }); !errors.Is(err, ErrFailed) {
		t.Errorf("unverified sign-in = %v, want ErrFailed", err)
	}
}

func TestReauthAcceptsOnlyTheAccountsPasskeys(t *testing.T) {
	s := newService(t)
	alice := User{ID: "eeeeeeeeeeee", Username: "alice"}
	device, key, credential := register(t, s, alice)
	alice.Credentials = []webauthn.Credential{*credential}
	mallory := User{ID: "ffffffffffff", Username: "mallory"}
	otherDevice, otherKey, otherCredential := register(t, s, mallory)
	mallory.Credentials = []webauthn.Credential{*otherCredential}

	reauth := func(device virtualwebauthn.Authenticator, key virtualwebauthn.Credential) (webauthn.SessionData, []byte) {
		t.Helper()
		assertion, session, err := s.BeginReauth(alice)
		if err != nil {
			t.Fatal(err)
		}
		// The browser is offered alice's passkeys only, and must verify her.
		if allowed := assertion.Response.AllowedCredentials; len(allowed) != 1 ||
			!slices.Equal(allowed[0].CredentialID, credential.ID) {
			t.Fatalf("allowed credentials = %+v, want alice's one passkey", allowed)
		}
		if assertion.Response.UserVerification != protocol.VerificationRequired {
			t.Errorf("reauth user verification = %q, want required", assertion.Response.UserVerification)
		}
		options, err := virtualwebauthn.ParseAssertionOptions(encode(t, assertion.Response))
		if err != nil {
			t.Fatal(err)
		}
		return *session, []byte(virtualwebauthn.CreateAssertionResponse(rp, device, key, *options))
	}

	session, answer := reauth(device, key)
	if _, err := s.FinishReauth(alice, session, answer); err != nil {
		t.Fatalf("FinishReauth: %v", err)
	}
	if _, err := s.FinishReauth(alice, session, answer); !errors.Is(err, ErrFailed) {
		t.Errorf("replayed reauth = %v, want ErrFailed", err)
	}
	// Another account's passkey never confirms alice.
	session, answer = reauth(otherDevice, otherKey)
	if _, err := s.FinishReauth(alice, session, answer); !errors.Is(err, ErrFailed) {
		t.Errorf("another account's passkey = %v, want ErrFailed", err)
	}
}
