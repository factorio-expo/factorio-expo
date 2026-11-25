// Spoiler/NSFW entries: marked on upload or in the details form, their
// previews hidden until revealed, and left out of the browse page unless asked.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { ready, uploadThroughUi } from "../support/ui.ts";

const notice = (page: Page) => page.getByText("This entry is marked as a spoiler or NSFW.");

test.describe("as member", () => {
	// The owner edits on the clean link (a guest's upload needs its edit link).
	test.use({ viewer: "member" });

	test("an upload marked as a spoiler hides its previews until revealed", async ({ page }) => {
		const title = `Spoiler ${uniqueWord()}`;
		await uploadThroughUi(page, { blueprint: BLUEPRINT, title, spoiler: true });

		await expect(notice(page)).toBeVisible();
		await expect(page.locator("[data-spoiler-gate]")).toHaveAttribute("data-hidden", "");
		await ready(page);
		await page.getByRole("button", { name: "Show previews" }).click();
		await expect(notice(page)).toBeHidden();
		await expect(page.locator("[data-spoiler-gate]")).not.toHaveAttribute("data-hidden");

		// The details form unmarks it.
		await page.getByRole("button", { name: "Edit details" }).click();
		const checkbox = page.getByRole("checkbox", { name: /Spoiler or NSFW/ });
		await expect(checkbox).toBeChecked();
		await checkbox.uncheck();
		await page.getByRole("button", { name: "Save" }).click();
		await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
		await expect(notice(page)).toBeHidden();
		await expect(page.locator("[data-spoiler-gate]")).toHaveCount(0);
	});
});

test("the browse page shows spoilers unless asked to hide them", async ({ page, seed }) => {
	const word = uniqueWord();
	const plain = await seed.entry({ title: `Plain ${word}` });
	const spoiler = await seed.entry({ title: `Hidden ${word}`, spoiler: true });
	await seed.searchable(plain, word);
	await seed.searchable(spoiler, word);

	await page.goto(`/browse?q=${word}`);
	await expect(page.getByRole("article")).toHaveCount(2);
	await expect(page.getByRole("heading", { name: spoiler.title })).toBeVisible();

	await page.getByText("Advanced search").click();
	await page.getByRole("checkbox", { name: "Hide spoiler/NSFW entries" }).check();
	await page.getByRole("button", { name: "Search blueprints" }).click();
	await expect(page).toHaveURL(/[?&]spoiler=hide/);
	await expect(page.getByRole("article")).toHaveCount(1);
	await expect(page.getByRole("heading", { name: plain.title })).toBeVisible();
	await expect(page.getByRole("checkbox", { name: "Hide spoiler/NSFW entries" })).toBeChecked();
});
