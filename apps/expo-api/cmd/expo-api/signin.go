package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"factorio-expo/apps/expo-api/internal/config"
	"factorio-expo/apps/expo-api/internal/identity"
	"factorio-expo/apps/expo-api/internal/passkey"
	"factorio-expo/apps/expo-api/internal/turnstile"
)

// signIn is how people sign in: the enabled providers, the passkey relying
// party and the sign-up human check.
type signIn struct {
	identities *identity.Registry
	passkeys   *passkey.Service
	// turnstile is the sign-up human check; nil when
	// API_TURNSTILE_SECRET_KEY is unset.
	turnstile *turnstile.Verifier
}

// turnstileTimeout bounds one siteverify call: sign-up waits for it.
const turnstileTimeout = 5 * time.Second

// newSignIn builds the sign-in services from the configuration: every
// provider's credentials are read by identity.Load.
func newSignIn(cfg config.Config) (signIn, error) {
	identities, err := identity.Load(cfg.PublicURL, os.Getenv, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		return signIn{}, err
	}
	names := []string{}
	for _, p := range identities.List() {
		names = append(names, p.Name)
	}
	slog.Info("sign-in providers", "enabled", names)
	passkeys, err := passkey.New(cfg.SiteURL)
	if err != nil {
		return signIn{}, err
	}
	site, err := url.Parse(cfg.SiteURL)
	if err != nil {
		return signIn{}, err
	}
	return signIn{identities: identities, passkeys: passkeys, turnstile: newTurnstile(cfg.TurnstileSecret, site.Hostname())}, nil
}

// newTurnstile turns the sign-up human check on when a secret is set. Tokens
// must come from a widget on hostname, the site's own. The web's
// PUBLIC_TURNSTILE_SITE_KEY must be set with it: without a widget every
// sign-up is refused.
func newTurnstile(secret, hostname string) *turnstile.Verifier {
	if secret == "" {
		slog.Info("sign-up human check off (API_TURNSTILE_SECRET_KEY unset)")
		return nil
	}
	if turnstile.IsTestSecret(secret) {
		slog.Warn("human check uses a Cloudflare Turnstile test key: never in production")
	}
	slog.Info("human check on (Turnstile)", "hostname", hostname)
	return turnstile.New(secret, hostname, &http.Client{Timeout: turnstileTimeout})
}
