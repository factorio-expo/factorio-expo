// Getting back into an account that lost every way to log in: the recovery
// code shown once at sign-up, and /recover, which trades the username and the
// code for a session and a new code. Accounts have no email, so the code is the
// only way back. Provider sign-ins are covered by the API's Go tests; here
// every account signs up with a passkey on Chromium's virtual authenticator.
import { readFile } from "node:fs/promises";
import type { Browser, Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { expect, expectSignedIn, expectSignedOut, header, test } from "../support/fixtures.ts";
import {
	RECOVERY_CODE,
	createAccountWithPasskey,
	shownRecoveryCode,
	signUpWithPasskey,
	useVirtualAuthenticator,
} from "../support/passkeys.ts";
import { ageSignIns, webURL } from "../support/stack.ts";
import { ready } from "../support/ui.ts";

const main = (page: Page) => page.getByRole("main");
const methods = (page: Page) => page.locator("[data-sign-in-methods]");
const confirmDialog = (page: Page) => page.getByRole("dialog", { name: "Confirm it's you" });
const noMatch = "That username and recovery code do not match.";

/** The person's new phone: a browser with no passkey and no session. */
async function newDevice(browser: Browser) {
	const context = await browser.newContext({
		baseURL: webURL,
		locale: "en-GB",
		timezoneId: "UTC",
		colorScheme: "light",
	});
	return { context, page: await context.newPage() };
}

/** Fills and submits the recovery form on /recover. */
async function recover(page: Page, username: string, code: string) {
	await page.goto("/recover");
	await ready(page);
	await main(page).getByLabel("Username").fill(username);
	await main(page).getByLabel("Recovery code").fill(code);
	await main(page).getByRole("button", { name: "Recover account" }).click();
}

/** A well-formed code that is not the account's: the last character changed. */
const wrongCode = (code: string) => `${code.slice(0, -1)}${code.endsWith("A") ? "B" : "A"}`;

test.describe("the recovery code at sign-up", () => {
	test("is shown after the account is created, with copy, download and continue", async ({
		page,
		context,
	}) => {
		await context.grantPermissions(["clipboard-read", "clipboard-write"]);
		await useVirtualAuthenticator(page);
		await page.goto("/login");
		await ready(page);
		const username = await createAccountWithPasskey(page);

		// Before the page the visitor came for, and signed in already.
		const code = await shownRecoveryCode(page);
		await expect(
			page.getByRole("heading", { level: 1, name: "Your recovery code" }),
		).toBeVisible();
		await expect(main(page)).toContainText("Your account is ready");
		await expect(main(page)).toContainText(`Recovery code for ${username}`);
		await expect(main(page)).toContainText("only way back in");
		await expectSignedIn(page, username);
		// The secret is never in the address.
		expect(page.url()).not.toContain(code.slice(0, 5));

		await main(page).getByRole("button", { name: "Copy" }).click();
		await expect(page.locator("[data-recovery-status]")).toHaveText("Copied to clipboard.");
		expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(code);

		const download = page.waitForEvent("download");
		await main(page).getByRole("button", { name: "Download" }).click();
		const saved = await download;
		expect(saved.suggestedFilename()).toBe(`factorio-expo-recovery-${username}.txt`);
		const text = await readFile(await saved.path(), "utf8");
		expect(text).toContain(`Username: ${username}`);
		expect(text).toContain(`Recovery code: ${code}`);
		expect(text).toContain(`${webURL}/recover`);

		// A reload shows the same code until the visitor continues.
		await page.reload();
		expect(await shownRecoveryCode(page)).toBe(code);

		await main(page).getByRole("button", { name: "Continue" }).click();
		await expect(page).toHaveURL("/");
		await expectSignedIn(page, username);

		// Continuing forgot it: nothing can show the code again.
		await page.goto("/recovery-code");
		await expect(main(page)).toContainText("There is no recovery code waiting here.");
		await expect(page.locator("[data-recovery-shown]")).toBeHidden();
		await expect(page.locator("[data-recovery-code]")).toBeHidden();
	});

	test("is kept in the tab that made the account, not offered to other tabs or visitors", async ({
		page,
		context,
		browser,
	}) => {
		await useVirtualAuthenticator(page);
		await page.goto("/login");
		await ready(page);
		await createAccountWithPasskey(page);
		const code = await shownRecoveryCode(page);

		const otherTab = await context.newPage();
		await otherTab.goto("/recovery-code");
		await expect(main(otherTab)).toContainText("There is no recovery code waiting here.");
		await expect(otherTab.locator("[data-recovery-code]")).toBeHidden();
		expect(await shownRecoveryCode(page)).toBe(code);

		const stranger = await newDevice(browser);
		try {
			await stranger.page.goto("/recovery-code");
			await expect(main(stranger.page)).toContainText(
				"There is no recovery code waiting here.",
			);
		} finally {
			await stranger.context.close();
		}
	});

	test("can be skipped: the account works without saving it", async ({ page }) => {
		await useVirtualAuthenticator(page);
		await page.goto("/login");
		await ready(page);
		const { username } = await signUpWithPasskey(page);
		await expect(page).toHaveURL("/");
		await expectSignedIn(page, username);
		await page.goto("/account");
		await expect(
			page.getByRole("heading", { level: 1, name: "Account settings" }),
		).toBeVisible();
	});

	test("every account gets a different code", async ({ seed }) => {
		const [first, second] = [await seed.account(), await seed.account()];
		expect(first.recoveryCode).toMatch(RECOVERY_CODE);
		expect(second.recoveryCode).toMatch(RECOVERY_CODE);
		expect(first.recoveryCode).not.toBe(second.recoveryCode);
	});
});

test.describe("recovering an account", () => {
	test("a person who lost their only passkey gets back in, replaces it and gets a new code", async ({
		page,
		browser,
	}) => {
		// The account is made on the first device, which is then lost.
		await useVirtualAuthenticator(page);
		await page.goto("/login");
		await ready(page);
		const { username, recoveryCode } = await signUpWithPasskey(page);
		await header(page).getByRole("button", { name: "Log out" }).click();
		await expectSignedOut(page);

		const phone = await newDevice(browser);
		try {
			await useVirtualAuthenticator(phone.page);
			await phone.page.goto("/login");
			await ready(phone.page);
			// The new device holds no passkey of the account: nothing to log in with.
			await main(phone.page).getByRole("button", { name: "Log in with a passkey" }).click();
			await expect(main(phone.page).getByRole("alert")).toContainText(
				"Passkey sign-in was cancelled or did not work.",
			);
			await expectSignedOut(phone.page);

			// The login page points to recovery; the code is typed sloppily
			// (any case, spaces for dashes) and so is the username.
			await main(phone.page)
				.getByRole("link", { name: "Recover it with your recovery code" })
				.click();
			await expect(phone.page).toHaveURL("/recover");
			await ready(phone.page);
			await main(phone.page).getByLabel("Username").fill(username.toUpperCase());
			await main(phone.page)
				.getByLabel("Recovery code")
				.fill(recoveryCode.toLowerCase().replaceAll("-", " "));
			await main(phone.page).getByRole("button", { name: "Recover account" }).click();

			// Signed in, and the code just used is replaced by a new one.
			const newCode = await shownRecoveryCode(phone.page);
			expect(newCode).not.toBe(recoveryCode);
			await expect(main(phone.page)).toContainText("You are back in");
			await expect(main(phone.page)).toContainText("this is your new recovery code");
			await expectSignedIn(phone.page, username);
			await main(phone.page).getByRole("button", { name: "Continue" }).click();
			await expect(phone.page).toHaveURL("/account");

			// The recovery is a fresh login: a new passkey is added at once,
			// with no "Confirm it's you", and the lost one removed.
			await ready(phone.page);
			await expect(methods(phone.page).getByText("Passkey 1")).toBeVisible();
			await methods(phone.page)
				.getByRole("button", { name: "Add a passkey on this device" })
				.click();
			await expect(methods(phone.page).getByText("Passkey 2")).toBeVisible();
			await expect(confirmDialog(phone.page)).toBeHidden();
			await methods(phone.page).getByRole("button", { name: "Remove passkey 1" }).click();
			await expect(methods(phone.page).getByText("Passkey 2")).toBeHidden();
			await expect(confirmDialog(phone.page)).toBeHidden();
			await expectSignedIn(phone.page, username);

			// The new passkey logs in; the old code is void, the new one works.
			await header(phone.page).getByRole("button", { name: "Log out" }).click();
			await expect(phone.page).toHaveURL("/");
			await phone.page.goto("/login?next=/account");
			await ready(phone.page);
			await main(phone.page).getByRole("button", { name: "Log in with a passkey" }).click();
			await expect(phone.page).toHaveURL("/account");
			await expectSignedIn(phone.page, username);
			await header(phone.page).getByRole("button", { name: "Log out" }).click();
			await expectSignedOut(phone.page);

			await recover(phone.page, username, recoveryCode);
			await expect(main(phone.page).getByRole("alert")).toHaveText(noMatch);
			await expect(phone.page).toHaveURL("/recover");
			await expectSignedOut(phone.page);

			await recover(phone.page, username, newCode);
			const thirdCode = await shownRecoveryCode(phone.page);
			expect([recoveryCode, newCode]).not.toContain(thirdCode);
			await expectSignedIn(phone.page, username);
		} finally {
			await phone.context.close();
		}
	});

	test("a code works once: the code it was replaced by takes over", async ({ browser, seed }) => {
		const account = await seed.account();
		const device = await newDevice(browser);
		try {
			await recover(device.page, account.username, account.recoveryCode);
			const second = await shownRecoveryCode(device.page);
			expect(second).not.toBe(account.recoveryCode);
			await device.page.getByRole("button", { name: "Continue" }).click();
			await expect(device.page).toHaveURL("/account");
			await header(device.page).getByRole("button", { name: "Log out" }).click();
			await expectSignedOut(device.page);

			await recover(device.page, account.username, account.recoveryCode);
			await expect(main(device.page).getByRole("alert")).toHaveText(noMatch);
			await expectSignedOut(device.page);

			await recover(device.page, account.username, second);
			expect(await shownRecoveryCode(device.page)).not.toBe(second);
			await expectSignedIn(device.page, account.username);
		} finally {
			await device.context.close();
		}
	});

	test("refuses every mismatch with the same words and uses nothing up", async ({
		page,
		seed,
	}) => {
		const alice = await seed.account();
		const bob = await seed.account();
		const attempts: [string, string, string][] = [
			["a wrong code", alice.username, wrongCode(alice.recoveryCode)],
			["another account's code", alice.username, bob.recoveryCode],
			["an unknown username", `nobody_${uniqueWord()}`, alice.recoveryCode],
			["a malformed code", alice.username, "not a code"],
			["a code that is too short", alice.username, alice.recoveryCode.slice(0, 20)],
			["a code that is too long", alice.username, `${alice.recoveryCode}A`],
			["the username as the code", alice.username, alice.username],
		];
		for (const [label, username, code] of attempts) {
			await recover(page, username, code);
			await expect(main(page).getByRole("alert"), label).toHaveText(noMatch);
			await expect(page, label).toHaveURL("/recover");
			await expectSignedOut(page);
		}

		// Nothing above used a code up.
		await recover(page, alice.username, alice.recoveryCode);
		await shownRecoveryCode(page);
		await expectSignedIn(page, alice.username);
	});

	test("asks for both fields before it asks the server", async ({ page }) => {
		await page.goto("/recover");
		await ready(page);
		await main(page).getByRole("button", { name: "Recover account" }).click();
		await expect(main(page).getByRole("alert")).toHaveText(
			"Enter your username and your recovery code.",
		);
		await main(page).getByLabel("Username").fill("someone");
		await main(page).getByRole("button", { name: "Recover account" }).click();
		await expect(main(page).getByRole("alert")).toHaveText(
			"Enter your username and your recovery code.",
		);
		await expect(page).toHaveURL("/recover");
	});

	test("does not log the account's other devices out", async ({ browser, seed }) => {
		const account = await seed.account();
		expect((await account.api.get("/api/v1/me")).status()).toBe(200);
		const device = await newDevice(browser);
		try {
			await recover(device.page, account.username, account.recoveryCode);
			await shownRecoveryCode(device.page);
			expect((await account.api.get("/api/v1/me")).status()).toBe(200);
		} finally {
			await device.context.close();
		}
	});

	test("cannot reach a deleted account", async ({ page, seed }) => {
		const account = await seed.account();
		expect((await account.api.delete("/api/v1/me")).status()).toBe(204);
		await recover(page, account.username, account.recoveryCode);
		await expect(main(page).getByRole("alert")).toHaveText(noMatch);
		await expectSignedOut(page);
	});

	test("is linked from the login page and links back to it", async ({ page }) => {
		await page.goto("/login");
		await main(page).getByRole("link", { name: "Recover it with your recovery code" }).click();
		await expect(page).toHaveURL("/recover");
		await expect(
			page.getByRole("heading", { level: 1, name: "Recover your account" }),
		).toBeVisible();
		await main(page).getByRole("link", { name: "Back to log in" }).click();
		await expect(page).toHaveURL("/login");
	});

	test.describe("signed in", () => {
		test.use({ viewer: "member" });

		test("sends a signed-in visitor home", async ({ page, me }) => {
			await page.goto("/recover");
			await expect(page).toHaveURL("/");
			await expectSignedIn(page, me.username);
		});
	});
});

const section = (page: Page) => page.locator("section[data-recovery-code]");
const generate = (page: Page) => page.getByRole("button", { name: "Generate a new recovery code" });

test.describe("generating a new recovery code on the account page", () => {
	test.use({ viewer: "member" });

	test("replaces the old code, which stops working, and shows the new one once", async ({
		page,
		me,
		browser,
	}) => {
		await page.goto("/account");
		await ready(page);
		await expect(page.getByRole("heading", { level: 2, name: "Recovery code" })).toBeVisible();
		await expect(section(page)).toContainText("Your account has a recovery code");
		await generate(page).click();

		// Fresh login (just signed in): no "Confirm it's you"; the code is shown once.
		const newCode = await shownRecoveryCode(page);
		expect(newCode).not.toBe(me.recoveryCode);
		await expect(main(page)).toContainText("Any earlier recovery code no longer works");
		await expect(confirmDialog(page)).toBeHidden();
		await page.reload();
		expect(await shownRecoveryCode(page)).toBe(newCode);
		await main(page).getByRole("button", { name: "Continue" }).click();
		await expect(page).toHaveURL("/account");
		await page.goto("/recovery-code");
		await expect(main(page)).toContainText("There is no recovery code waiting here.");

		// The old code is void, the new one works (on another device).
		const device = await newDevice(browser);
		try {
			await recover(device.page, me.username, me.recoveryCode);
			await expect(main(device.page).getByRole("alert")).toHaveText(noMatch);
			await recover(device.page, me.username, newCode);
			await shownRecoveryCode(device.page);
			await expectSignedIn(device.page, me.username);
		} finally {
			await device.context.close();
		}
		// This session stayed signed in.
		expect((await me.api.get("/api/v1/me")).status()).toBe(200);
	});

	test("asks to confirm it's you after a while, then shows the new code", async ({
		page,
		me,
	}) => {
		await useVirtualAuthenticator(page);
		await page.goto("/account");
		await ready(page);
		// A passkey on this device to confirm with.
		await methods(page).getByRole("button", { name: "Add a passkey on this device" }).click();
		await expect(methods(page).getByText("Passkey 2")).toBeVisible();

		ageSignIns(me.username);
		await ready(page);
		await generate(page).click();
		await expect(confirmDialog(page)).toBeVisible();
		await expect(page).toHaveURL("/account");

		await confirmDialog(page).getByRole("button", { name: "Use a passkey" }).click();
		await expect(confirmDialog(page)).toBeHidden();
		const newCode = await shownRecoveryCode(page);
		expect(newCode).not.toBe(me.recoveryCode);
	});
});
