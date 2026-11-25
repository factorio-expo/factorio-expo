// The owner's own entry list: the way back to a private or unlisted entry,
// which nothing public lists.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { expect, test } from "../support/fixtures.ts";

test.use({ viewer: "member" });

const entryRows = (page: Page) => page.getByRole("main");
const row = (page: Page, title: string) =>
	page.getByRole("article").filter({ has: page.getByRole("link", { name: title }) });

test("lists every upload with its visibility, and only the public one is public", async ({
	page,
	seed,
	me,
}) => {
	const word = uniqueWord();
	const shown = await seed.entry({ title: `Shown ${word}`, owner: me });
	const byLink = await seed.entry({
		title: `By link ${word}`,
		owner: me,
		visibility: "unlisted",
	});
	const secret = await seed.entry({ title: `Secret ${word}`, owner: me, visibility: "private" });
	const someoneElses = await seed.entry({ title: `Other ${word}`, owner: await seed.account() });

	await page.goto("/account");
	await page
		.getByRole("navigation", { name: "Sidebar" })
		.getByRole("link", { name: "My entries" })
		.click();
	await expect(page).toHaveURL("/account/entries");
	await expect(page.getByRole("heading", { level: 1, name: "My entries" })).toBeVisible();
	// Newest first, each with its visibility.
	await expect(page.getByRole("article")).toHaveCount(3);
	await expect(page.getByRole("article").first()).toContainText(secret.title);
	await expect(row(page, secret.title)).toContainText("Private");
	await expect(row(page, byLink.title)).toContainText("Unlisted");
	await expect(row(page, shown.title)).not.toContainText(/Private|Unlisted/);
	await expect(entryRows(page).getByRole("link", { name: someoneElses.title })).toBeHidden();

	await row(page, secret.title).getByRole("link", { name: secret.title }).click();
	await expect(page).toHaveURL(`/e/${secret.id}`);
	await expect(page.getByRole("heading", { level: 1, name: secret.title })).toBeVisible();

	await page.goto(`/u/${me.username}`);
	await expect(page.getByRole("heading", { level: 2, name: shown.title })).toBeVisible();
	await expect(page.getByRole("heading", { level: 2, name: secret.title })).toBeHidden();
	await expect(page.getByRole("heading", { level: 2, name: byLink.title })).toBeHidden();

	await seed.searchable(shown, word);
	await page.goto(`/browse?q=${encodeURIComponent(word)}`);
	await expect(page.getByRole("heading", { level: 2, name: shown.title })).toBeVisible();
	await expect(page.getByRole("heading", { level: 2, name: secret.title })).toBeHidden();
});

test("an empty entry list says so", async ({ page }) => {
	await page.goto("/account/entries");
	await expect(page.getByText("You have not uploaded anything yet.")).toBeVisible();
});
