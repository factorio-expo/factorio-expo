// Publishing an anonymous entry to an account (claiming it): the same panel
// and edit-link banner as a quick preview, but an anonymous entry is claimed
// as it is, without waiting for its render.
import type { Page } from "@playwright/test";
import { signIn, uniqueWord } from "../support/api.ts";
import { BLUEPRINT, relabel } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { signUpWithPasskey, useVirtualAuthenticator } from "../support/passkeys.ts";
import { entryDetails, openPublishForm, ready, uploadThroughUi } from "../support/ui.ts";

const panel = (page: Page) => page.getByRole("region", { name: "Anonymous entry" });

test("an edit-link holder creates an account from the banner and publishes the entry", async ({
	page,
}) => {
	await useVirtualAuthenticator(page);
	const title = `Claim ${uniqueWord()}`;
	const id = await uploadThroughUi(page, { blueprint: relabel(BLUEPRINT, title), title });

	// The clean link shows no panel; the banner offers signing in to publish.
	await expect(panel(page)).toBeHidden();
	await page.getByRole("status").getByRole("link", { name: "Sign in to publish" }).click();
	await ready(page);
	await signUpWithPasskey(page);

	await expect(page).toHaveURL(new RegExp(`/e/${id}\\?token=`));
	await expect(panel(page).getByLabel("Entry title")).toBeHidden();
	await openPublishForm(panel(page));
	await expect(panel(page)).toContainText(
		"only your account can edit or delete it, and the edit link stops working",
	);
	await expect(panel(page).getByLabel("Entry title")).toHaveValue(title);
	await panel(page).getByLabel("Entry title").fill(`${title} mine`);
	await panel(page).getByLabel("Visibility").selectOption("Private (only you)");
	// No render needed: the button is enabled at once.
	await panel(page).getByRole("button", { name: "Publish" }).click();

	await expect(page).toHaveURL(new RegExp(`/e/${id}$`));
	await expect(page.getByRole("heading", { level: 1, name: `${title} mine` })).toBeVisible();
	await expect(panel(page)).toBeHidden();
	await page.goto("/account/entries");
	await ready(page);
	await expect(page.getByText(`${title} mine`)).toBeVisible();
});

test("publishing an expiring entry keeps its expiry unless changed", async ({ page, seed }) => {
	const account = await seed.account();
	const title = `Claim expiring ${uniqueWord()}`;
	const id = await uploadThroughUi(page, {
		blueprint: relabel(BLUEPRINT, title),
		title,
		expires: "After 14 days",
	});
	// The banner script fills the link in after load.
	await expect(page.getByLabel("Edit link")).toHaveValue(/\?token=/);
	const editLink = await page.getByLabel("Edit link").inputValue();
	await signIn(page, account);
	await page.goto(editLink);
	await openPublishForm(panel(page));
	await expect(panel(page).getByLabel("Expires")).toHaveValue("");
	await expect(panel(page).getByLabel("Expires").locator("option:checked")).toHaveText(
		/Keep: \d{4}-\d{2}-\d{2}/,
	);
	await panel(page).getByRole("button", { name: "Publish" }).click();

	await expect(page).toHaveURL(new RegExp(`/e/${id}$`));
	await expect(entryDetails(page)).toContainText(/\(in 13 days, 23 hr\)/);
});

test("the edit link alone offers only signing in", async ({ page, seed }) => {
	const entry = await seed.entry();
	await page.goto(`/e/${entry.id}?token=${entry.editToken}`);
	await expect(panel(page)).toContainText("Anyone with its edit link can edit or delete it.");
	await expect(panel(page)).toContainText("the edit link stops working for good");
	await expect(panel(page).getByRole("link", { name: "Sign in to publish" })).toBeVisible();
	await expect(panel(page).getByRole("button", { name: "Publish" })).toBeHidden();
	// Visitors without the link see no panel.
	await page.goto(`/e/${entry.id}`);
	await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
	await expect(panel(page)).toBeHidden();
});

test("an owned entry offers no publishing, even with a stale token", async ({ page, seed }) => {
	const owner = await seed.account();
	const entry = await seed.entry({ owner });
	await signIn(page, owner);
	await page.goto(`/e/${entry.id}?token=stale`);
	await expect(page.getByRole("button", { name: "Edit details" })).toBeVisible();
	await expect(panel(page)).toBeHidden();
});
