// The response headers every page of both apps carries. Astro writes the
// per-page script/style CSP itself (astro.config.mjs `security.csp`); this
// adds a second, enforced policy for framing, base and form targets.

/** Sets the shared security headers; each app adds its Referrer-Policy and caching. */
export function setSecurityHeaders(headers: Headers): void {
	headers.set("X-Content-Type-Options", "nosniff");
	headers.set("X-Frame-Options", "DENY");
	headers.append(
		"Content-Security-Policy",
		"frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'",
	);
	headers.set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()");
}
