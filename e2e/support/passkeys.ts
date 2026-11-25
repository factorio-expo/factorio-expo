// Passkeys in the suite: Chromium's virtual authenticator (CDP WebAuthn
// domain) answers navigator.credentials at once, as a phone or Windows Hello
// would after the person confirms. Every passkey is real: the site's own
// ceremony creates it and the API verifies it. Chromium only (docs/e2e.md).
import { expect, type Page } from "@playwright/test";
import { uniqueWord } from "./api.ts";
import { ready } from "./ui.ts";

/** Gives the page a platform authenticator that stores discoverable passkeys and always verifies the user. */
export async function useVirtualAuthenticator(page: Page): Promise<void> {
	const cdp = await page.context().newCDPSession(page);
	await cdp.send("WebAuthn.enable");
	await cdp.send("WebAuthn.addVirtualAuthenticator", {
		options: {
			protocol: "ctap2",
			transport: "internal",
			hasResidentKey: true,
			hasUserVerification: true,
			isUserVerified: true,
			automaticPresenceSimulation: true,
		},
	});
}

/**
 * Creates an account through the login page's passkey sign-up. The page must
 * be on /login with a virtual authenticator; it then follows `next`.
 */
export async function createAccountWithPasskey(
	page: Page,
	username = uniqueWord("user_"),
): Promise<string> {
	await page.getByRole("radio", { name: "I'm new here" }).check();
	await page.getByLabel("Username").fill(username);
	await page.getByRole("button", { name: "Create account with a passkey" }).click();
	return username;
}

/** A recovery code as the site shows it: 26 characters in groups of five. */
export const RECOVERY_CODE = /^[A-HJ-NP-Z2-9]{5}(-[A-HJ-NP-Z2-9]{5}){4}-[A-HJ-NP-Z2-9]$/;

/** The recovery code the page is showing (a sign-up or a recovery just made it). */
export async function shownRecoveryCode(page: Page): Promise<string> {
	await expect(page).toHaveURL("/recovery-code");
	const code = page.locator("[data-recovery-code]");
	await expect(code).toHaveText(RECOVERY_CODE);
	return (await code.textContent()) ?? "";
}

/**
 * Reads the recovery code the page shows, then clicks Continue, as a person
 * who saved it would; resolves to the code.
 */
export async function continuePastRecoveryCode(page: Page): Promise<string> {
	const code = await shownRecoveryCode(page);
	await page.getByRole("button", { name: "Continue" }).click();
	await expect(page).not.toHaveURL("/recovery-code");
	// The page it leads to is script-driven (log-out button, forms).
	await ready(page);
	return code;
}

/**
 * Creates an account through the login page's passkey sign-up and gets past
 * the recovery code page, which every new account sees first. Resolves to the
 * username and the code shown.
 */
export async function signUpWithPasskey(
	page: Page,
	username?: string,
): Promise<{ username: string; recoveryCode: string }> {
	const name = await createAccountWithPasskey(page, username);
	return { username: name, recoveryCode: await continuePastRecoveryCode(page) };
}
