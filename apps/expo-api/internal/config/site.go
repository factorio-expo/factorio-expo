package config

import (
	"errors"
	"fmt"
	"net/url"
)

// checkSite reads the site's origin (the first API_ALLOWED_ORIGIN) and checks
// it against API_PUBLIC_URL and the cookie setting.
func (c *Config) checkSite() error {
	origins := splitOrigins(c.AllowedOrigin)
	if len(origins) == 0 {
		return fmt.Errorf("API_ALLOWED_ORIGIN must name the site's origin first")
	}
	site, err := url.Parse(origins[0])
	if err != nil || site.Host == "" || (site.Scheme != "http" && site.Scheme != "https") {
		return fmt.Errorf("API_ALLOWED_ORIGIN's first origin %q is not an http(s) origin", origins[0])
	}
	if err := checkPublicURLs(c.CookieSecure, c.PublicURL, origins[0]); err != nil {
		return err
	}
	c.Origins, c.SiteURL = origins, origins[0]
	return nil
}

// checkPublicURLs refuses a public URL that Secure cookies cannot work on.
// With Secure cookies on, API_PUBLIC_URL and the site origin must be https
// unless loopback (local compose and e2e run Secure cookies on
// http://localhost). A loopback API_PUBLIC_URL next to a real site origin is
// the production mistake of leaving the localhost default in place.
func checkPublicURLs(cookieSecure bool, publicURL, siteURL string) error {
	api, err := url.Parse(publicURL)
	if err != nil || api.Host == "" || (api.Scheme != "http" && api.Scheme != "https") {
		return fmt.Errorf("API_PUBLIC_URL %q is not an http(s) URL", publicURL)
	}
	site, err := url.Parse(siteURL)
	if err != nil {
		return fmt.Errorf("API_ALLOWED_ORIGIN's first origin %q: %w", siteURL, err)
	}
	if isLoopback(api.Hostname()) && !isLoopback(site.Hostname()) {
		return fmt.Errorf("API_PUBLIC_URL %q is loopback but the site %q is not: set API_PUBLIC_URL to the API's public https URL", publicURL, siteURL)
	}
	if !cookieSecure {
		return nil
	}
	for _, check := range []struct {
		name string
		u    *url.URL
	}{{"API_PUBLIC_URL", api}, {"API_ALLOWED_ORIGIN's first origin", site}} {
		if check.u.Scheme != "https" && !isLoopback(check.u.Hostname()) {
			return fmt.Errorf("%s %q must be https while API_COOKIE_SECURE is on", check.name, check.u.String())
		}
	}
	return nil
}

// devTypesenseKey is the compose default for local development and the only
// key it gives Typesense unless .env sets TYPESENSE_API_KEY.
const devTypesenseKey = "local-dev-typesense-key"

// checkTypesenseKey refuses a missing Typesense key, and the compose
// development default anywhere but a loopback site (production must set a
// random TYPESENSE_API_KEY). origins is API_ALLOWED_ORIGIN; the first is the
// site.
func checkTypesenseKey(key, origins string) error {
	if key == "" {
		return errors.New("TYPESENSE_API_KEY is required")
	}
	if key != devTypesenseKey {
		return nil
	}
	list := splitOrigins(origins)
	if len(list) == 0 {
		return nil // the origin check reports this
	}
	site, err := url.Parse(list[0])
	if err != nil || !isLoopback(site.Hostname()) {
		return fmt.Errorf("TYPESENSE_API_KEY is the development default %q but the site %q is not local: set a random key", devTypesenseKey, list[0])
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// checkImportStub refuses API_IMPORT_STUB_ORIGIN (which lifts the import
// fetcher's address checks) unless the whole stack is local and not
// production-like: every allowed origin and the public URL must be loopback,
// and Secure cookies (the production setting) must be off.
func (c Config) checkImportStub() error {
	if c.ImportStubOrigin == "" {
		return nil
	}
	stub, err := url.Parse(c.ImportStubOrigin)
	if err != nil || (stub.Scheme != "http" && stub.Scheme != "https") || stub.Host == "" {
		return errors.New("API_IMPORT_STUB_ORIGIN must be an http(s) origin")
	}
	if c.CookieSecure {
		return errors.New("API_IMPORT_STUB_ORIGIN is for local stacks only, but API_COOKIE_SECURE is on (set it false for the local stack)")
	}
	origins := splitOrigins(c.AllowedOrigin)
	if len(origins) == 0 {
		return nil // the origin check reports this
	}
	for _, origin := range origins {
		site, err := url.Parse(origin)
		if err != nil || !isLoopback(site.Hostname()) {
			return fmt.Errorf("API_IMPORT_STUB_ORIGIN is for local stacks only, but the allowed origin %q is not local", origin)
		}
	}
	if public, err := url.Parse(c.PublicURL); err != nil || !isLoopback(public.Hostname()) {
		return fmt.Errorf("API_IMPORT_STUB_ORIGIN is for local stacks only, but API_PUBLIC_URL %q is not local", c.PublicURL)
	}
	return nil
}
