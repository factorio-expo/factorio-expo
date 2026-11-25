// Security headers (the shared set plus the site's referrer and caching rules)
// for every page, after settling who is viewing.
import { defineMiddleware } from "astro:middleware";
import { setSecurityHeaders } from "@factorio-expo/ui/utils/security-headers.ts";

import { dropSessionCookies, sessionToken } from "@lib/api/server";
import { anonymousCacheControl } from "@lib/page-cache";
import { getViewer } from "@lib/viewer";

export const onRequest = defineMiddleware(async (context, next) => {
	// Prerendered pages are static files at runtime; the reverse proxy adds their headers.
	if (context.isPrerendered) return next();
	const hadSession = Boolean(sessionToken(context.cookies));
	// Who is viewing is settled before the page renders (the layout and page
	// reuse the answer). A session the API no longer accepts (revoked by a
	// logout elsewhere, expired) is dropped: the page's own API calls then go
	// out anonymous instead of being refused, and the browser stops sending it.
	if (hadSession && (await getViewer(context)).state === "out") {
		dropSessionCookies(context.cookies);
	}
	const response = await next();
	const headers = response.headers;
	setSecurityHeaders(headers);
	// Edit links carry ?token=; never leak a URL (and its token) to other sites.
	headers.set("Referrer-Policy", "same-origin");

	const hasEditToken = context.url.searchParams.has("token");
	// Pages rendered for a signed-in visitor or an edit-token holder are
	// personal: shared caches (CDN, reverse proxy) must never store them.
	if (hasEditToken || hadSession) {
		headers.set("Cache-Control", "private, no-store");
	} else if (response.status === 200 && !headers.has("Cache-Control")) {
		const cacheControl = anonymousCacheControl(context.url.pathname);
		if (cacheControl) headers.set("Cache-Control", cacheControl);
	}
	if (hasEditToken) headers.set("X-Robots-Tag", "noindex, nofollow");
	return response;
});
