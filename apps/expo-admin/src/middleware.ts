// Every request: the CSRF origin check, the staff member, and the console's
// security headers. An identity-aware proxy is the gate in front of the console; it
// signs every request it lets through, and this middleware verifies that
// signature (src/lib/access.ts) before trusting anyone. See docs/admin.md.
import {
	ADMIN_ACCESS_AUD,
	ADMIN_ACCESS_TEAM_DOMAIN,
	ADMIN_DEV_USER,
	ADMIN_ORIGIN,
} from "astro:env/server";
import { defineMiddleware } from "astro:middleware";
import { setSecurityHeaders } from "@factorio-expo/ui/utils/security-headers.ts";

import { createAccessVerifier, isLocalOrigin, type StaffResult } from "@lib/access";
import { isAllowedOrigin } from "@lib/request-guards";

const consoleOrigin = new URL(ADMIN_ORIGIN).origin;

// With Access configured, only a verified token signs anyone in. Without it,
// the one exception is a development user on a local origin; anything else
// refuses every request (the console never runs open).
const identify: (headers: Headers) => Promise<StaffResult> = (() => {
	if (ADMIN_ACCESS_TEAM_DOMAIN && ADMIN_ACCESS_AUD) {
		return createAccessVerifier({
			teamDomain: ADMIN_ACCESS_TEAM_DOMAIN,
			audience: ADMIN_ACCESS_AUD,
		});
	}
	const devUser = ADMIN_DEV_USER?.trim();
	if (devUser && isLocalOrigin(ADMIN_ORIGIN)) {
		return async () => ({ ok: true, username: devUser });
	}
	return async () => ({
		ok: false,
		reason: "Access is not configured (ADMIN_ACCESS_TEAM_DOMAIN and ADMIN_ACCESS_AUD)",
	});
})();

function secure(response: Response): Response {
	const headers = response.headers;
	setSecurityHeaders(headers);
	headers.set("Referrer-Policy", "no-referrer");
	headers.set("Cache-Control", "no-store");
	headers.set("X-Robots-Tag", "noindex, nofollow");
	return response;
}

export const onRequest = defineMiddleware(async (context, next) => {
	const { request } = context;
	if (!isAllowedOrigin(request.method, request.headers, consoleOrigin)) {
		return secure(new Response("Cross-origin request refused", { status: 403 }));
	}
	const staff = await identify(request.headers);
	if (!staff.ok) {
		// The reason goes to the log, never to the client.
		console.error(`console sign-in refused: ${staff.reason}`);
		return secure(new Response("Not signed in", { status: 401 }));
	}
	context.locals.staff = { username: staff.username };
	return secure(await next());
});
