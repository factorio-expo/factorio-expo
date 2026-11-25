import type { ApiClient } from "../http-client";
import { ok } from "../http-client";
import type { PasskeyAssertion, PasskeyAttestation, SignupRequest } from "../types";

/** A sign-up form's body; the token is left out while the human check is off. */
const signupBody = (username: string, turnstileToken: string): SignupRequest =>
	turnstileToken ? { username, turnstile_token: turnstileToken } : { username };

/**
 * Sign-in without passwords (apps/expo-api internal/httpapi/auth_*.go): a
 * provider sign-in is a browser navigation to `providerStartPath`; passkeys
 * are begin/finish pairs around navigator.credentials (lib/passkeys.ts).
 * The passkey options and credentials are WebAuthn JSON: the generated types
 * describe them, and the callers hand them to @simplewebauthn/browser.
 */
export function createAuthApi(http: ApiClient) {
	return {
		providers: () => ok(http.api.GET("/api/v1/auth/providers")),
		/** The provider sign-up waiting for a username (404 when none). */
		pendingSignup: () => ok(http.api.GET("/api/v1/auth/signup")),
		/** `turnstileToken`: the form's human check token, "" while it is off. */
		completeSignup: (username: string, turnstileToken = "") =>
			ok(
				http.api.POST("/api/v1/auth/signup", {
					body: signupBody(username, turnstileToken),
				}),
			),
		beginPasskeyLogin: () => ok(http.api.POST("/api/v1/auth/passkeys/login/begin")),
		finishPasskeyLogin: (credential: PasskeyAssertion) =>
			ok(http.api.POST("/api/v1/auth/passkeys/login/finish", { body: credential })),
		beginPasskeySignup: (username: string, turnstileToken = "") =>
			ok(
				http.api.POST("/api/v1/auth/passkeys/signup/begin", {
					body: signupBody(username, turnstileToken),
				}),
			),
		finishPasskeySignup: (credential: PasskeyAttestation) =>
			ok(http.api.POST("/api/v1/auth/passkeys/signup/finish", { body: credential })),
		/**
		 * Signs in with the username and recovery code of an account that lost its
		 * sign-ins (401 `recovery_failed` for any mismatch). Uses the code up: the
		 * answer carries the new one.
		 */
		recover: (username: string, recoveryCode: string) =>
			ok(
				http.api.POST("/api/v1/auth/recover", {
					body: { username, recovery_code: recoveryCode },
				}),
			),
		/** Replaces the account's recovery code (needs a recent login); the answer carries the new one. */
		newRecoveryCode: () => ok(http.api.POST("/api/v1/me/recovery-code")),
		logout: () => ok(http.api.POST("/api/v1/auth/logout")),
		me: () => ok(http.api.GET("/api/v1/me")),
		signInMethods: () => ok(http.api.GET("/api/v1/me/sign-in-methods")),
		removeIdentity: (provider: string) =>
			ok(
				http.api.DELETE("/api/v1/me/identities/{provider}", {
					params: { path: { provider } },
				}),
			),
		removePasskey: (passkeyId: string) =>
			ok(
				http.api.DELETE("/api/v1/me/passkeys/{passkeyID}", {
					params: { path: { passkeyID: passkeyId } },
				}),
			),
		beginAddPasskey: () => ok(http.api.POST("/api/v1/me/passkeys/begin")),
		finishAddPasskey: (credential: PasskeyAttestation) =>
			ok(http.api.POST("/api/v1/me/passkeys/finish", { body: credential })),
		/** "Confirm it's you" with one of this account's passkeys. */
		beginPasskeyReauth: () => ok(http.api.POST("/api/v1/me/reauth/passkey/begin")),
		finishPasskeyReauth: (credential: PasskeyAssertion) =>
			ok(http.api.POST("/api/v1/me/reauth/passkey/finish", { body: credential })),
		/**
		 * Everything the account holds, as one JSON document to save (needs a
		 * recent login: 403 reauth_required). Never parsed here: read as a Blob.
		 */
		exportData: () => ok(http.api.GET("/api/v1/me/export", { parseAs: "blob" })),
		/** Deletes the caller's own account: every session and sign-in method goes at once. */
		deleteAccount: () => ok(http.api.DELETE("/api/v1/me")),
		sessions: () => ok(http.api.GET("/api/v1/me/sessions")),
		endSession: (sessionId: string) =>
			ok(
				http.api.DELETE("/api/v1/me/sessions/{sessionID}", {
					params: { path: { sessionID: sessionId } },
				}),
			),
		/** Signs out everywhere, this device included. */
		endAllSessions: () => ok(http.api.DELETE("/api/v1/me/sessions")),
	};
}

export type AuthApi = ReturnType<typeof createAuthApi>;

/**
 * The API path that starts a provider sign-in (a navigation, not a fetch):
 * `next` is where the browser lands afterwards; `link` adds the provider to
 * the signed-in account instead, and `reauth` confirms the signed-in person
 * ("confirm it's you"); both return to /account. Pass it through
 * assetUrl (lib/api/public-asset.ts).
 */
export function providerStartPath(
	provider: string,
	options: { next?: string; link?: boolean; reauth?: boolean },
) {
	const query = new URLSearchParams();
	if (options.link) query.set("link", "1");
	else if (options.reauth) query.set("reauth", "1");
	else if (options.next && options.next !== "/") query.set("next", options.next);
	const search = query.size > 0 ? `?${query}` : "";
	return `/api/v1/auth/${encodeURIComponent(provider)}/start${search}`;
}
