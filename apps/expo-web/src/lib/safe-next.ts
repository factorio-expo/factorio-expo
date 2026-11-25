// Post-login destinations. Pure so the server (login redirect) and the browser
// (auth form) share one rule.

/**
 * Only same-origin paths are valid destinations. The value is resolved the way
 * the browser will resolve it (which strips tabs/newlines, so "/\t/evil.com"
 * would become "//evil.com"), and anything that lands on another origin falls
 * back to "/".
 */
export function safeNext(raw: string | null | undefined, origin: string): string {
	if (!raw || !raw.startsWith("/")) return "/";
	try {
		const url = new URL(raw, origin);
		if (url.origin !== origin) return "/";
		return url.pathname + url.search + url.hash;
	} catch {
		return "/";
	}
}

export function loginPath(next: string): string {
	return `/login?next=${encodeURIComponent(next)}`;
}
