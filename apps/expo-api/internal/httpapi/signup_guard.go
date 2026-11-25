package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"factorio-expo/apps/expo-api/internal/fault"
	"factorio-expo/apps/expo-api/internal/turnstile"
)

// Sign-up guards: creating an account (POST /api/v1/auth/signup, the passkey
// sign-up begin and finish) is checked cheapest first:
//
//  1. the sign-in budget per request, and the address's account-creation
//     budget, looked at but not drawn from (withRegistrationBudget);
//  2. the handler's flow cookie and username checks;
//  3. Cloudflare Turnstile (humanCheck), when it is on; the passkey begin
//     records the pass in its flow cookie, so the finish needs no token;
//  4. once the account exists, the account-creation budget is charged
//     (chargeRegistration).
//
// So cookie-less floods and "username taken" retries cost the address
// nothing, and nothing is budgeted site-wide.

// Turnstile actions, one per widget: the API accepts a token only for the
// form it was made on.
const (
	turnstileActionProvider = "signup-provider"
	turnstileActionPasskey  = "signup-passkey"
)

// Error codes of a refused human check (writeCodedError).
const (
	turnstileFailedCode      = "turnstile_failed"
	turnstileUnavailableCode = "turnstile_unavailable"
)

// registrationChecks are the account-creation budgets of r's address: the
// address (IPv6 by /64) and, for IPv6, its /48.
func (s *server) registrationChecks(r *http.Request) []check {
	addr := s.clientIPs.clientIP(r)
	checks := []check{{ruleRegisterIP, clientKey(addr)}}
	if addr.Is6() {
		network, _ := addr.Prefix(48)
		checks = append(checks, check{ruleRegisterIP6Net, network.String()})
	}
	return checks
}

// withRegistrationBudget refuses a sign-up step (429) when the address may
// create no more accounts for now, without drawing from the budget.
func (s *server) withRegistrationBudget(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter != nil {
			for _, c := range s.registrationChecks(r) {
				if ok, wait := s.limiter.Peek(c.rule, c.key); !ok {
					tooManyRequests(w, wait)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// chargeRegistration draws the account-creation budget for an account just
// created from r's address.
func (s *server) chargeRegistration(r *http.Request) {
	if s.limiter == nil {
		return
	}
	for _, c := range s.registrationChecks(r) {
		s.limiter.Allow(c.rule, c.key)
	}
}

// verifyHuman is humanCheck as an error (turnstile_failed, a 403, or
// turnstile_unavailable, a 503), for Huma operations (requestFrom).
func (s *server) verifyHuman(r *http.Request, token, action string) error {
	if s.turnstile == nil {
		return nil
	}
	remoteIP := ""
	if addr := s.clientIPs.clientIP(r); !addr.IsUnspecified() {
		remoteIP = addr.String()
	}
	err := s.turnstile.Verify(r.Context(), token, action, remoteIP)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, turnstile.ErrUnavailable):
		slog.Error("turnstile check unavailable", "error", err)
		return fault.New(fault.Unavailable, turnstileUnavailableCode,
			"the human check is unavailable right now, try again in a few minutes")
	default:
		slog.Info("turnstile check failed", "action", action, "error", err)
		return fault.New(fault.Forbidden, turnstileFailedCode, "the human check did not pass, try again")
	}
}
