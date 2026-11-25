// Identity-aware proxy sign-in (docs/admin.md). The proxy sits in front of the
// console and sends every request a signed JWT in `Cf-Access-Jwt-Assertion`;
// this module verifies it (signature against the team's published keys,
// issuer, audience, expiry) and names the staff member by the `email` claim.
// Nothing here trusts a header just because it is present.
import { createRemoteJWKSet, errors, jwtVerify, type JWTVerifyGetKey } from "jose";

export const ACCESS_JWT_HEADER = "Cf-Access-Jwt-Assertion";

/** The staff API accepts usernames of 1-64 characters (internal/staff). */
const MAX_USERNAME_LENGTH = 64;

export type AccessConfig = {
	/** The proxy's team domain, e.g. myteam.example.com. */
	teamDomain: string;
	/** The Access application's audience (AUD) tag. */
	audience: string;
};

export type StaffResult =
	| { ok: true; username: string }
	/** `reason` is for the server log only, never for the client. */
	| { ok: false; reason: string };

/** A team domain as the bare host: tolerates a pasted https:// and a trailing slash. */
export function normalizeTeamDomain(value: string): string {
	return value
		.trim()
		.replace(/^https?:\/\//i, "")
		.replace(/\/+$/, "");
}

/** Where *Log out* goes: Access ends its own session, then shows its login. */
export function accessLogoutUrl(teamDomain: string): string {
	return `https://${normalizeTeamDomain(teamDomain)}/cdn-cgi/access/logout`;
}

/** The team's signing keys (cached and refreshed by jose). */
export function remoteKeys(teamDomain: string): JWTVerifyGetKey {
	const host = normalizeTeamDomain(teamDomain);
	return createRemoteJWKSet(new URL(`https://${host}/cdn-cgi/access/certs`));
}

/**
 * Builds the request check for one Access application. `keys` is injectable
 * so tests use local keys instead of the network.
 */
export function createAccessVerifier(
	config: AccessConfig,
	keys: JWTVerifyGetKey = remoteKeys(config.teamDomain),
): (headers: Headers) => Promise<StaffResult> {
	const issuer = `https://${normalizeTeamDomain(config.teamDomain)}`;
	return async (headers) => {
		const token = headers.get(ACCESS_JWT_HEADER)?.trim();
		if (!token)
			return { ok: false, reason: "no Access token (request did not come through Access)" };
		try {
			const { payload } = await jwtVerify(token, keys, {
				issuer,
				audience: config.audience,
				algorithms: ["RS256"],
			});
			// Service tokens carry no email; the console is for people.
			const email = typeof payload.email === "string" ? payload.email.trim() : "";
			if (!email) return { ok: false, reason: "token has no email claim" };
			if (email.length > MAX_USERNAME_LENGTH) {
				return { ok: false, reason: "email claim longer than the staff username limit" };
			}
			return { ok: true, username: email };
		} catch (error) {
			return { ok: false, reason: describeFailure(error) };
		}
	};
}

function describeFailure(error: unknown): string {
	if (error instanceof errors.JWTExpired) return "token expired";
	if (error instanceof errors.JWTClaimValidationFailed) {
		return `token claim rejected (${error.claim}: ${error.reason})`;
	}
	if (error instanceof errors.JWSSignatureVerificationFailed) return "token signature invalid";
	if (error instanceof errors.JWKSNoMatchingKey) return "no matching Access signing key";
	if (error instanceof errors.JOSEError) return `token invalid (${error.code})`;
	return `could not verify the token: ${error instanceof Error ? error.message : String(error)}`;
}

/** localhost, 127.0.0.1, [::1] and *.localhost: where the dev bypass may run. */
export function isLocalOrigin(origin: string): boolean {
	let host: string;
	try {
		host = new URL(origin).hostname;
	} catch {
		return false;
	}
	return (
		host === "localhost" ||
		host.endsWith(".localhost") ||
		host === "127.0.0.1" ||
		host === "[::1]" ||
		host === "::1"
	);
}
