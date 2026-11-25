import { readFile } from "node:fs/promises";
import type { Page } from "@playwright/test";
import {
	expect,
	expectSignedIn,
	expectSignedOut,
	header,
	sidebar,
	test,
} from "../support/fixtures.ts";
import {
	continuePastRecoveryCode,
	createAccountWithPasskey,
	useVirtualAuthenticator,
} from "../support/passkeys.ts";
import { ageSignIns, banAccount, purgeDeletedAccount } from "../support/stack.ts";
import { ready } from "../support/ui.ts";

test.use({ viewer: "member" });

const settings = (page: Page) => page.locator("[data-settings]");
const methods = (page: Page) => page.locator("[data-sign-in-methods]");
const sessions = (page: Page) => page.locator("[data-sessions]");
const yourData = (page: Page) => page.locator("[data-your-data]");
const confirmDialog = (page: Page) => page.getByRole("dialog", { name: "Confirm it's you" });
const deleteSection = (page: Page) => page.locator("[data-delete-account]");
const deleteDialog = (page: Page) => page.getByRole("dialog", { name: "Delete your account?" });

test("shows the account's own details", async ({ page, me }) => {
	await page.goto("/");
	await sidebar(page).getByRole("link", { name: "Account settings" }).click();
	await expect(page).toHaveURL("/account");
	await expect(page.getByRole("heading", { level: 1, name: "Account settings" })).toBeVisible();
	await expect(settings(page)).toContainText(me.username);

	await settings(page)
		.getByRole("link", { name: `/u/${me.username}` })
		.click();
	await expect(page).toHaveURL(`/u/${me.username}`);
	await expect(page.getByRole("heading", { level: 1, name: me.username })).toBeVisible();
	await expect(page.getByText("No public entries yet.")).toBeVisible();
});

test("keeps the only sign-in method and asks for a second one", async ({ page }) => {
	await page.goto("/account");
	await expect(methods(page).getByText("Passkey 1")).toBeVisible();
	await expect(methods(page).getByRole("note")).toContainText(
		"If you lose it, you will need your recovery code. Add a second passkey or link a provider.",
	);
	await expect(methods(page).getByText("There is no email or password to reset")).toBeHidden();
	await expect(methods(page).getByRole("button", { name: "Remove passkey 1" })).toBeDisabled();
});

test("adds a passkey on this device, signs in with it and removes the old one", async ({
	page,
	me,
}) => {
	await useVirtualAuthenticator(page);
	await page.goto("/account");
	await ready(page);
	await methods(page).getByRole("button", { name: "Add a passkey on this device" }).click();
	await expect(methods(page).getByText("Passkey 2")).toBeVisible();
	await expect(methods(page).getByRole("note")).toBeHidden();
	await expect(methods(page).getByText("There is no email or password to reset")).toBeVisible();

	// The new passkey signs this browser back in.
	await header(page).getByRole("button", { name: "Log out" }).click();
	await expect(page).toHaveURL("/");
	await expectSignedOut(page);
	await page.goto("/login?next=/account");
	await ready(page);
	await page.getByRole("main").getByRole("button", { name: "Log in with a passkey" }).click();
	await expect(page).toHaveURL("/account");
	await expectSignedIn(page, me.username);

	// The seeded passkey (from another device) can go now.
	await ready(page);
	await methods(page).getByRole("button", { name: "Remove passkey 1" }).click();
	await expect(methods(page).getByText("Passkey 2")).toBeHidden();
	await expect(methods(page).getByRole("note")).toBeVisible();
});

test("asks to confirm it's you before changing sign-in methods after a while", async ({
	page,
	me,
}) => {
	await useVirtualAuthenticator(page);
	await page.goto("/account");
	await ready(page);
	// Just logged in: no confirmation needed.
	await methods(page).getByRole("button", { name: "Add a passkey on this device" }).click();
	await expect(methods(page).getByText("Passkey 2")).toBeVisible();

	// An hour later the same kind of change asks to log in again first.
	ageSignIns(me.username);
	await ready(page);
	await methods(page).getByRole("button", { name: "Remove passkey 1" }).click();
	await expect(confirmDialog(page)).toBeVisible();
	await expect(methods(page).getByText("Passkey 2")).toBeVisible();

	// The passkey on this device confirms it, and the removal goes through.
	await confirmDialog(page).getByRole("button", { name: "Use a passkey" }).click();
	await expect(confirmDialog(page)).toBeHidden();
	await expect(methods(page).getByText("Passkey 2")).toBeHidden();
	await expect(methods(page).getByRole("note")).toBeVisible();
	await expectSignedIn(page, me.username);
	// Removing a sign-in method logs every other device out.
	expect((await me.api.get("/api/v1/me")).status()).toBe(401);
});

test("lists where you're logged in and logs another device out", async ({ page, me }) => {
	await page.goto("/account");
	await ready(page);
	await expect(sessions(page).getByText("This device")).toBeVisible();
	const others = sessions(page).getByRole("button", { name: /^Log out session/ });
	await expect(others).toHaveCount(1);

	await others.click();
	await expect(others).toHaveCount(0);
	await expect(sessions(page).getByText("This device")).toBeVisible();
	expect((await me.api.get("/api/v1/me")).status()).toBe(401);
	await expectSignedIn(page, me.username);
});

test("logs out everywhere", async ({ page, me }) => {
	await page.goto("/account");
	await ready(page);
	await sessions(page).getByRole("button", { name: "Log out everywhere" }).click();
	await expect(page).toHaveURL("/");
	await expectSignedOut(page);
	expect((await me.api.get("/api/v1/me")).status()).toBe(401);
});

test("downloads the account's data, after confirming it's you", async ({ page, me }) => {
	await useVirtualAuthenticator(page);
	await page.goto("/account");
	await ready(page);
	// The seeded passkey lives on another device: add one here to confirm with later.
	await methods(page).getByRole("button", { name: "Add a passkey on this device" }).click();
	await expect(methods(page).getByText("Passkey 2")).toBeVisible();

	// Just logged in: the download starts at once.
	const exportButton = yourData(page).getByRole("button", { name: "Export my data" });
	let download = page.waitForEvent("download");
	await exportButton.click();
	const saved = await download;
	expect(saved.suggestedFilename()).toMatch(
		new RegExp(String.raw`^factorio-expo-${me.username}-\d{4}-\d{2}-\d{2}\.json$`),
	);
	const data: unknown = JSON.parse(await readFile(await saved.path(), "utf8"));
	expect(data).toMatchObject({
		format: "factorio-expo-account-export",
		account: { username: me.username },
		passkeys: [{}, {}],
	});
	await expect(yourData(page).getByRole("status")).toHaveText("Your data is downloaded.");

	// An hour later it asks to log in again first, then downloads.
	ageSignIns(me.username);
	download = page.waitForEvent("download");
	await exportButton.click();
	await expect(confirmDialog(page)).toBeVisible();
	await confirmDialog(page).getByRole("button", { name: "Use a passkey" }).click();
	await expect(confirmDialog(page)).toBeHidden();
	expect((await download).suggestedFilename()).toContain(me.username);
});

test("explains what deleting the account does and asks for the username", async ({ page, me }) => {
	await page.goto("/account");
	await ready(page);
	await deleteSection(page).getByRole("button", { name: "Delete account" }).click();
	const dialog = deleteDialog(page);
	await expect(dialog).toBeVisible();
	// It opens at the top: the explanation first, the username field untouched.
	await expect(dialog.getByText("This happens at once:")).toBeInViewport();
	await expect(dialog.getByLabel("Type your username to confirm")).not.toBeFocused();
	await expect(dialog).toContainText("You are logged out on every device");
	await expect(dialog).toContainText("Nobody can log in to this account again.");
	await expect(dialog).toContainText("Entries you uploaded without logging in stay");
	await expect(dialog).toContainText("Seven days later everything left of the account is erased");

	// The button waits for the right username; Cancel deletes nothing.
	const confirm = dialog.getByRole("button", { name: "Delete my account" });
	await expect(confirm).toBeDisabled();
	await dialog.getByLabel("Type your username to confirm").fill(`${me.username}x`);
	await expect(confirm).toBeDisabled();
	await dialog.getByLabel("Type your username to confirm").fill(me.username);
	await expect(confirm).toBeEnabled();
	await dialog.getByRole("button", { name: "Cancel" }).click();
	await expect(dialog).toBeHidden();
	expect((await me.api.get("/api/v1/me")).status()).toBe(200);
	await expectSignedIn(page, me.username);
});

test("deletes the account: every session ends and its passkey no longer logs in", async ({
	page,
	me,
	seed,
}) => {
	await useVirtualAuthenticator(page);
	await page.goto("/account");
	await ready(page);
	// A passkey on this device, to try logging back in with afterwards.
	await methods(page).getByRole("button", { name: "Add a passkey on this device" }).click();
	await expect(methods(page).getByText("Passkey 2")).toBeVisible();
	const entry = await seed.entry({ owner: me, visibility: "public" });
	const anonymous = await seed.entry({ visibility: "public" });
	const other = await seed.account();
	const commented = await seed.entry({ owner: other, visibility: "public" });
	const number = await seed.comment(me, commented, "Nice build");

	// An hour later deleting asks to confirm it's you first.
	ageSignIns(me.username);
	await deleteSection(page).getByRole("button", { name: "Delete account" }).click();
	await deleteDialog(page).getByLabel("Type your username to confirm").fill(me.username);
	await deleteDialog(page).getByRole("button", { name: "Delete my account" }).click();
	await expect(confirmDialog(page)).toBeVisible();
	await confirmDialog(page).getByRole("button", { name: "Use a passkey" }).click();

	await expect(page).toHaveURL("/account-deleted");
	await expect(
		page.getByRole("heading", { level: 1, name: "Your account is deleted" }),
	).toBeVisible();
	await expectSignedOut(page);
	// The other device's token is dead too, and so is the account area.
	expect((await me.api.get("/api/v1/me")).status()).toBe(401);
	await page.goto("/account");
	await expect(page).toHaveURL(/\/login/);

	// The passkey on this device is not registered any more.
	await page.goto("/login");
	await ready(page);
	await page.getByRole("main").getByRole("button", { name: "Log in with a passkey" }).click();
	await expect(page.getByRole("main")).toContainText("This passkey is not registered here.");
	await expectSignedOut(page);

	// Its profile and entries are gone; its comment is a tombstone; an
	// anonymous entry is untouched.
	expect((await page.goto(`/u/${me.username}`))?.status()).toBe(404);
	expect((await page.goto(`/e/${entry.id}`))?.status()).toBe(404);
	expect((await page.goto(`/e/${anonymous.id}`))?.status()).toBe(200);
	await page.goto(`/e/${commented.id}`);
	await expect(page.locator(`#c-${number}`)).toContainText(
		"Removed: its author deleted their account.",
	);
	await expect(page.getByText("Nice build")).toBeHidden();
});

/** Deletes the signed-in account from the account page (a fresh login: no confirmation). */
async function deleteAccount(page: Page, username: string) {
	await page.goto("/account");
	await ready(page);
	await deleteSection(page).getByRole("button", { name: "Delete account" }).click();
	await deleteDialog(page).getByLabel("Type your username to confirm").fill(username);
	await deleteDialog(page).getByRole("button", { name: "Delete my account" }).click();
	await expect(page).toHaveURL("/account-deleted");
}

test("a banned account that deletes itself cannot sign up again under its name", async ({
	page,
	me,
	seed,
}) => {
	banAccount(me.username);
	await deleteAccount(page, me.username);
	await useVirtualAuthenticator(page);
	const notAvailable = "That username is not available. Choose another one.";
	const signUp = async (username: string) => {
		await page.goto("/login");
		await ready(page);
		await createAccountWithPasskey(page, username);
	};

	// For the week before the hard delete, the account row holds the name.
	await signUp(me.username);
	await expect(page.getByRole("main").getByRole("alert")).toHaveText(notAvailable);

	// After it (the row and its ban are gone), the ban hold still refuses the
	// name, in any spelling that differs only in case, '_' or '-'. The page
	// says only "not available", as for any taken name.
	purgeDeletedAccount(me.username);
	for (const name of [me.username, me.username.replace("_", "").toUpperCase()]) {
		await signUp(name);
		await expect(page.getByRole("main").getByRole("alert")).toHaveText(notAvailable);
		await expectSignedOut(page);
	}

	// An account that was not banned frees its name at the hard delete.
	const other = await seed.account();
	expect((await other.api.delete("/api/v1/me")).status()).toBe(204);
	purgeDeletedAccount(other.username);
	await signUp(other.username);
	await continuePastRecoveryCode(page);
	await expectSignedIn(page, other.username);
});
