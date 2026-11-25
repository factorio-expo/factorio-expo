// Passkey ceremonies in the browser: the API's begin call answers WebAuthn
// options, @simplewebauthn/browser runs navigator.credentials with them, the
// finish call verifies the answer. Import only from client-side code.
import {
	browserSupportsWebAuthn,
	startAuthentication,
	startRegistration,
} from "@simplewebauthn/browser";
import type {
	PublicKeyCredentialCreationOptionsJSON,
	PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";
import type { getBrowserApiClient } from "@lib/api/browser";
import type { PasskeyAssertion, PasskeyAttestation } from "@lib/api/types";

type Api = ReturnType<typeof getBrowserApiClient>;

// The API passes the WebAuthn library's options and the browser's credential
// through untouched. Their generated schemas are looser than
// @simplewebauthn's JSON types (plain strings where those have unions), so
// the two meet here: the options up, the credential down.
// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- see above
const requestOptions = (options: unknown) => options as PublicKeyCredentialRequestOptionsJSON;
// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- see above
const creationOptions = (options: unknown) => options as PublicKeyCredentialCreationOptionsJSON;
// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- see above
const assertion = (credential: unknown) => credential as PasskeyAssertion;
// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- see above
const attestation = (credential: unknown) => credential as PasskeyAttestation;

/** Whether this browser can use passkeys at all. */
export const passkeysSupported = browserSupportsWebAuthn;

/** Signs in with any passkey of this site (the browser asks which). */
export async function signInWithPasskey(api: Api) {
	const optionsJSON = requestOptions(await api.auth.beginPasskeyLogin());
	return api.auth.finishPasskeyLogin(assertion(await startAuthentication({ optionsJSON })));
}

/**
 * Creates an account named `username` with a new passkey, and signs it in.
 * `turnstileToken` is the form's human check token ("" while it is off).
 */
export async function signUpWithPasskey(api: Api, username: string, turnstileToken: string) {
	const optionsJSON = creationOptions(
		await api.auth.beginPasskeySignup(username, turnstileToken),
	);
	return api.auth.finishPasskeySignup(attestation(await startRegistration({ optionsJSON })));
}

/** Adds a passkey on this device to the signed-in account. */
export async function addPasskey(api: Api) {
	const optionsJSON = creationOptions(await api.auth.beginAddPasskey());
	await api.auth.finishAddPasskey(attestation(await startRegistration({ optionsJSON })));
}

/** "Confirm it's you" with one of the signed-in account's passkeys. */
export async function confirmWithPasskey(api: Api) {
	const optionsJSON = requestOptions(await api.auth.beginPasskeyReauth());
	await api.auth.finishPasskeyReauth(assertion(await startAuthentication({ optionsJSON })));
}
