// Package passkey runs the WebAuthn ceremonies (go-webauthn) for passkey
// sign-up, sign-in, confirming a signed-in person and adding a passkey to an
// account. It holds no state: the caller keeps the SessionData between a
// Begin and its Finish (a signed flow cookie) and stores credentials
// (internal/accounts).
package passkey

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// ErrFailed is returned when the browser's answer does not verify.
var ErrFailed = errors.New("passkey check failed")

// User is an account as WebAuthn sees it. The user handle is the account id
// (random, not personal), so a discoverable sign-in finds the account.
type User struct {
	ID          string
	Username    string
	Credentials []webauthn.Credential
}

func (u User) WebAuthnID() []byte                         { return []byte(u.ID) }
func (u User) WebAuthnName() string                       { return u.Username }
func (u User) WebAuthnDisplayName() string                { return u.Username }
func (u User) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// challengeLifetime outlives the flow cookie that carries a challenge, so a
// challenge is remembered for as long as it could be presented.
const challengeLifetime = 10 * time.Minute

// Service runs ceremonies for one relying party (the site).
type Service struct {
	webauthn *webauthn.WebAuthn
	// used are the challenges already presented, so a captured cookie and
	// answer cannot be replayed (the API is one process).
	mu   sync.Mutex
	used map[string]time.Time
}

// New configures the relying party: its id is the site's host name and the
// only browser origin the ceremonies may run on is the site's own (not the
// rest of the CORS list).
func New(siteURL string) (*Service, error) {
	site, err := url.Parse(siteURL)
	if err != nil || site.Hostname() == "" {
		return nil, fmt.Errorf("site URL %q has no host", siteURL)
	}
	w, err := webauthn.New(&webauthn.Config{
		RPID:          site.Hostname(),
		RPDisplayName: "Factorio Expo",
		RPOrigins:     []string{siteURL},
		// Passkeys must be discoverable (sign in without a username) and
		// every ceremony must verify the person (PIN, biometrics): a passkey
		// is the account's only factor. go-webauthn applies this setting to
		// registration and sign-in alike and refuses an answer without the
		// UV flag.
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
	if err != nil {
		return nil, fmt.Errorf("configure passkeys: %w", err)
	}
	return &Service{webauthn: w, used: map[string]time.Time{}}, nil
}

// claim marks a ceremony's challenge used; false if it was used before.
func (s *Service) claim(session webauthn.SessionData) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for challenge, expires := range s.used {
		if now.After(expires) {
			delete(s.used, challenge)
		}
	}
	if _, seen := s.used[session.Challenge]; seen || session.Challenge == "" {
		return false
	}
	s.used[session.Challenge] = now.Add(challengeLifetime)
	return true
}

var errReplayed = errors.New("challenge already used")

// failed wraps a ceremony error in ErrFailed. go-webauthn keeps the useful
// part (such as which origin the browser sent) in DevInfo, which its Error()
// leaves out, so it is added for the log.
func failed(err error) error {
	var detail *protocol.Error
	if errors.As(err, &detail) && detail.DevInfo != "" {
		return fmt.Errorf("%w: %v (%s)", ErrFailed, err, detail.DevInfo)
	}
	return fmt.Errorf("%w: %v", ErrFailed, err)
}

// BeginRegistration starts creating a passkey for user; the user's existing
// passkeys are excluded so one authenticator is not registered twice.
func (s *Service) BeginRegistration(user User) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	exclusions := webauthn.Credentials(user.Credentials).CredentialDescriptors()
	return s.webauthn.BeginRegistration(user, webauthn.WithExclusions(exclusions))
}

// FinishRegistration verifies the browser's new credential (body is its
// JSON) for the user BeginRegistration was called with.
func (s *Service) FinishRegistration(user User, session webauthn.SessionData, body []byte) (*webauthn.Credential, error) {
	if !s.claim(session) {
		return nil, fmt.Errorf("%w: %v", ErrFailed, errReplayed)
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return nil, failed(err)
	}
	credential, err := s.webauthn.CreateCredential(user, session, parsed)
	if err != nil {
		return nil, failed(err)
	}
	return credential, nil
}

// BeginLogin starts a sign-in with any passkey of this site (discoverable:
// no username asked).
func (s *Service) BeginLogin() (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	return s.webauthn.BeginDiscoverableLogin()
}

// FinishLogin verifies the browser's assertion (body is its JSON). lookup
// loads the account named by the passkey's user handle with its passkeys.
// It returns the account and the credential with its new sign count and
// flags, for the caller to store.
func (s *Service) FinishLogin(session webauthn.SessionData, body []byte, lookup func(userID string) (User, error)) (User, *webauthn.Credential, error) {
	parsed, err := s.parseAssertion(session, body)
	if err != nil {
		return User{}, nil, err
	}
	var found User
	credential, err := s.webauthn.ValidateDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		user, err := lookup(string(userHandle))
		if err != nil {
			return nil, err
		}
		found = user
		return user, nil
	}, session, parsed)
	if err != nil {
		return User{}, nil, failed(err)
	}
	if err := checkClone(credential); err != nil {
		return User{}, nil, err
	}
	if !bytes.Equal(found.WebAuthnID(), parsed.Response.UserHandle) {
		return User{}, nil, fmt.Errorf("%w: user handle mismatch", ErrFailed)
	}
	return found, credential, nil
}

// BeginReauth starts a sign-in limited to user's own passkeys, to confirm
// that a signed-in person is still the account's owner ("confirm it's
// you"): the browser offers no other account's passkey.
func (s *Service) BeginReauth(user User) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	return s.webauthn.BeginLogin(user)
}

// FinishReauth verifies an assertion made with one of user's passkeys (the
// user BeginReauth was called with) and returns the credential with its new
// sign count and flags, for the caller to store.
func (s *Service) FinishReauth(user User, session webauthn.SessionData, body []byte) (*webauthn.Credential, error) {
	parsed, err := s.parseAssertion(session, body)
	if err != nil {
		return nil, err
	}
	credential, err := s.webauthn.ValidateLogin(user, session, parsed)
	if err != nil {
		return nil, failed(err)
	}
	if err := checkClone(credential); err != nil {
		return nil, err
	}
	return credential, nil
}

// parseAssertion claims the ceremony's challenge and parses the browser's
// assertion JSON.
func (s *Service) parseAssertion(session webauthn.SessionData, body []byte) (*protocol.ParsedCredentialAssertionData, error) {
	if !s.claim(session) {
		return nil, fmt.Errorf("%w: %v", ErrFailed, errReplayed)
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return nil, failed(err)
	}
	return parsed, nil
}

// checkClone refuses a credential whose counter went backwards, which means
// a copied authenticator. Synced passkeys always report 0 and never trip it.
func checkClone(credential *webauthn.Credential) error {
	if credential.Authenticator.CloneWarning {
		return fmt.Errorf("%w: sign counter went backwards (cloned authenticator?)", ErrFailed)
	}
	return nil
}
