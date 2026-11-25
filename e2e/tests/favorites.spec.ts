import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BOOK } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { bookLeaf, holdScripts, ready } from "../support/ui.ts";

test.use({ viewer: "member" });

const card = (page: Page, title: string) =>
	page.getByRole("article").filter({ has: page.getByRole("heading", { name: title }) });
const favoritesList = (page: Page) => page.getByRole("main");

test("lists favorites and opens one", async ({ page, seed, me }) => {
	const first = await seed.entry({ title: `Fav one ${uniqueWord()}` });
	const second = await seed.entry({ title: `Fav two ${uniqueWord()}` });
	await seed.favorite(me, first);
	await seed.favorite(me, second);

	await page.goto("/account");
	await page
		.getByRole("navigation", { name: "Sidebar" })
		.getByRole("link", { name: "Favorites" })
		.click();
	await expect(page.getByRole("heading", { level: 1, name: "Favorites" })).toBeVisible();
	await expect(page.getByRole("article")).toHaveCount(2);

	await card(page, first.title).getByRole("link", { name: first.title }).first().click();
	await expect(page).toHaveURL(`/e/${first.id}`);
});

// The entry's own panel: a leaf page also lists similar blueprints, whose cards have favorite buttons too.
const panel = (page: Page) => page.getByRole("region", { name: "Entry details" });

test("an empty favorites list says so", async ({ page }) => {
	await page.goto("/account/favorites");
	await expect(page.getByText("You have not favorited anything yet.")).toBeVisible();
});

test("marks and removes a favorite on the entry page", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Starred ${uniqueWord()}` });
	await page.goto(`/e/${entry.id}`);

	await panel(page).getByRole("button", { name: "Add to favorites" }).click();
	await expect(
		panel(page).getByRole("button", { name: "Remove from favorites" }),
	).toHaveAttribute("aria-pressed", "true");
	await page.reload();
	await expect(panel(page).getByRole("button", { name: "Remove from favorites" })).toBeVisible();

	await page.goto("/account/favorites");
	await expect(favoritesList(page).getByRole("link", { name: entry.title })).toBeVisible();

	await page.goto(`/e/${entry.id}`);
	await panel(page).getByRole("button", { name: "Remove from favorites" }).click();
	await expect(panel(page).getByRole("button", { name: "Add to favorites" })).toHaveAttribute(
		"aria-pressed",
		"false",
	);
	await page.goto("/account/favorites");
	await expect(page.getByText("You have not favorited anything yet.")).toBeVisible();
});

test("marks and removes a favorite on a blueprint inside a book", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Starred book ${uniqueWord()}`, blueprint: BOOK });
	await page.goto(`/e/${entry.id}`);
	await (await bookLeaf(page, "Biochamber cell")).click();
	await expect(page.getByRole("heading", { level: 1, name: "Biochamber cell" })).toBeVisible();
	await ready(page);

	await panel(page).getByRole("button", { name: "Add to favorites" }).click();
	await expect(panel(page).getByRole("button", { name: "Remove from favorites" })).toBeVisible();
	// A favorite is the whole entry, so the entry page agrees.
	await page.getByRole("link", { name: "← Back to entry" }).click();
	await expect(panel(page).getByRole("button", { name: "Remove from favorites" })).toBeVisible();
});

test("marks and removes a favorite on an entry card", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Card ${uniqueWord()}` });
	await seed.searchable(entry);
	await page.goto(`/browse?q=${encodeURIComponent(entry.title)}`);
	const toggle = card(page, entry.title).getByRole("button", { name: /favorites$/ });

	await expect(toggle).toHaveAccessibleName("Add to favorites");
	await expect(toggle).toHaveText("0");
	await toggle.click();
	await expect(toggle).toHaveAccessibleName("Remove from favorites");
	await expect(toggle).toHaveText("1");

	await page.goto("/account/favorites");
	await expect(favoritesList(page).getByRole("link", { name: entry.title })).toBeVisible();

	await page.goto(`/browse?q=${encodeURIComponent(entry.title)}`);
	await expect(toggle).toHaveAccessibleName("Remove from favorites");
	await toggle.click();
	await expect(toggle).toHaveAccessibleName("Add to favorites");
	await expect(toggle).toHaveText("0");
});

test("every page shows the viewer's favorite before its scripts run", async ({
	page,
	seed,
	me,
}) => {
	const owner = await seed.account();
	const entry = await seed.entry({
		title: `Rendered fav ${uniqueWord()}`,
		blueprint: BOOK,
		owner,
	});
	const collection = await seed.collection(owner, { visibility: "public" });
	await seed.addToCollection(owner, collection, entry);
	await seed.favorite(me, entry);

	const release = await holdScripts(page);
	await page.goto(`/e/${entry.id}`, { waitUntil: "commit" });
	await expect(panel(page).getByRole("button", { name: "Remove from favorites" })).toBeVisible();

	const leafHref = await (await bookLeaf(page, "Biochamber cell")).getAttribute("href");
	await page.goto(leafHref!, { waitUntil: "commit" });
	await expect(page.getByRole("heading", { level: 1, name: "Biochamber cell" })).toBeVisible();
	await expect(panel(page).getByRole("button", { name: "Remove from favorites" })).toBeVisible();

	for (const listing of [`/u/${owner.username}`, `/collections/${collection.id}`]) {
		await page.goto(listing, { waitUntil: "commit" });
		await expect(
			card(page, entry.title).getByRole("button", { name: "Remove from favorites" }),
		).toBeVisible();
	}
	release();
});

test("removes a favorite from the favorites list", async ({ page, seed, me }) => {
	const kept = await seed.entry({ title: `Kept ${uniqueWord()}` });
	const dropped = await seed.entry({ title: `Dropped ${uniqueWord()}` });
	await seed.favorite(me, kept);
	await seed.favorite(me, dropped);

	await page.goto("/account/favorites");
	// Matches either state: the accessible name flips on click, so a name-specific locator would lose the button.
	const star = card(page, dropped.title).getByRole("button", { name: /favorites$/ });
	await star.click();
	await expect(star).toHaveAccessibleName("Add to favorites");

	// The card leaves the list on the next load.
	await page.reload();
	await expect(page.getByRole("article")).toHaveCount(1);
	await expect(favoritesList(page).getByRole("link", { name: kept.title })).toBeVisible();
	await expect(favoritesList(page).getByRole("link", { name: dropped.title })).toBeHidden();
	await page.goto(`/e/${dropped.id}`);
	await expect(panel(page).getByRole("button", { name: "Add to favorites" })).toBeVisible();
});
