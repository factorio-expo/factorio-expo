// Server-side (SSR) API client factory. Import this only from .astro
// frontmatter or other server-only code — it reads the internal,
// compose-network base URL, which is never valid from a browser.
import type { AstroCookies } from "astro";
import { API_INTERNAL_BASE_URL } from "astro:env/server";
import { PUBLIC_API_BASE_URL } from "astro:env/client";

import { editTokenOf } from "@lib/entry-links";

import { createApiClient } from "./index";

/**
 * The URL SSR reaches the API at. A relative PUBLIC_API_BASE_URL ("/") names
 * no server, so it needs API_INTERNAL_BASE_URL.
 */
function ssrApiBase(): string {
	const base = API_INTERNAL_BASE_URL || PUBLIC_API_BASE_URL;
	if (!/^https?:\/\//.test(base)) {
		throw new Error("API_INTERNAL_BASE_URL must be set when PUBLIC_API_BASE_URL is relative");
	}
	return base;
}

/** SSR gives up on an API call after this long; the admin console does the same. */
const SSR_TIMEOUT_MS = 10_000;

/** The API's session cookie names (Secure/__Host- in production, plain in local HTTP dev). */
export const SESSION_COOKIES = ["__Host-expo_session", "expo_session"] as const;

/** The parts of the Astro global the client needs to act for the visitor. */
export interface ServerRequestContext {
	cookies: AstroCookies;
	request: Request;
	clientAddress: string;
}

/** The visitor's session token from the HttpOnly cookie, if any. */
export function sessionToken(cookies: AstroCookies): string | undefined {
	for (const name of SESSION_COOKIES) {
		const value = cookies.get(name)?.value;
		if (value) return value;
	}
	return undefined;
}

/**
 * The API's pending sign-up cookie names: a provider identity waiting for its
 * username on /signup.
 */
const SIGNUP_COOKIES = ["__Host-expo_signup", "expo_signup"] as const;

/**
 * Expires API cookies in the browser and hides them from the rest of this
 * request. Attributes match the API's own cookies (`__Host-` requires Secure
 * and Path=/); a cookie the visitor does not have is left alone.
 */
function dropCookies(cookies: AstroCookies, names: readonly string[]) {
	for (const name of names) {
		if (!cookies.has(name)) continue;
		cookies.delete(name, {
			path: "/",
			httpOnly: true,
			sameSite: "lax",
			secure: name.startsWith("__Host-"),
		});
	}
}

/** Drops the session cookie, so later `sessionToken` calls see no session. */
export function dropSessionCookies(cookies: AstroCookies) {
	dropCookies(cookies, SESSION_COOKIES);
}

/**
 * Drops a pending provider sign-up: whoever opens the login page is starting
 * over, so an abandoned sign-up must not linger in a shared browser.
 */
export function dropSignupCookies(cookies: AstroCookies) {
	dropCookies(cookies, SIGNUP_COOKIES);
}

/**
 * The visitor's address for API rate limiting. The reverse proxy sets X-Real-IP
 * from the real client address; without a proxy (local dev) the socket
 * address is used. The API only trusts this header from private-network peers.
 */
function visitorAddress(context: ServerRequestContext): string | undefined {
	const forwarded = context.request.headers.get("x-real-ip")?.trim();
	if (forwarded) return forwarded;
	try {
		return context.clientAddress;
	} catch {
		return undefined;
	}
}

/**
 * Builds an API client for use during server rendering. With the Astro
 * request context (omit it on prerendered pages) it acts as the visitor: their session cookie becomes a bearer token, the
 * page URL's edit token travels with every request, and their address is
 * forwarded. Prefers API_INTERNAL_BASE_URL (the compose-network address of
 * expo-api) and falls back to the public URL so `astro dev` works without
 * extra configuration.
 */
export function getServerApiClient(context?: ServerRequestContext) {
	const address = context && visitorAddress(context);
	return createApiClient({
		baseUrl: ssrApiBase(),
		token: context && sessionToken(context.cookies),
		editToken: context && editTokenOf(context.request.url),
		headers: address ? { "X-Real-IP": address } : undefined,
		timeoutMs: SSR_TIMEOUT_MS,
	});
}
