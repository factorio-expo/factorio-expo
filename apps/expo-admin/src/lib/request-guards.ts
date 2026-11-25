const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

/**
 * The CSRF check for state-changing requests: the browser must say the
 * request comes from the console's own origin. Sec-Fetch-Site, set by the
 * browser and never by a page, decides when present: the console's
 * `Referrer-Policy: no-referrer` makes browsers send `Origin: null` on its
 * own form posts. Without it, Origin must be the console's; a request with
 * neither is refused.
 */
export function isAllowedOrigin(method: string, headers: Headers, consoleOrigin: string): boolean {
	if (SAFE_METHODS.has(method.toUpperCase())) return true;
	const site = headers.get("Sec-Fetch-Site");
	if (site !== null) return site === "same-origin";
	return headers.get("Origin") === consoleOrigin;
}
