import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BOOK } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { bookLeaf, ready } from "../support/ui.ts";

test.use({ viewer: "member" });

const collectionRows = (page: Page) => page.locator("[data-collection-rows]");
const entryRows = (page: Page) => page.locator("[data-collection] ol.entries");

async function openPicker(page: Page) {
	await ready(page);
	await page.getByRole("button", { name: "Add to collection" }).click();
	const dialog = page.getByRole("dialog", { name: "Add to collection" });
	await expect(dialog).toBeVisible();
	await expect(dialog.getByText("Loading collections...")).toBeHidden();
	return dialog;
}

test("lists the account's collections", async ({ page, seed, me }) => {
	const shared = await seed.collection(me, { visibility: "public" });
	const hidden = await seed.collection(me, { visibility: "private" });
	await seed.collection(await seed.account()); // someone else's

	await page.goto("/account");
	await page
		.getByRole("navigation", { name: "Sidebar" })
		.getByRole("link", { name: "Collections" })
		.click();
	await expect(page.getByRole("heading", { level: 1, name: "Collections" })).toBeVisible();
	await expect(collectionRows(page).getByRole("listitem")).toHaveCount(2);
	await expect(
		collectionRows(page).getByRole("listitem").filter({ hasText: shared.name }),
	).toContainText("Public");
	await expect(
		collectionRows(page).getByRole("listitem").filter({ hasText: hidden.name }),
	).toContainText("Private");
});

test("an account without collections is told so", async ({ page }) => {
	await page.goto("/account/collections");
	await expect(page.getByText("You have no collections yet.")).toBeVisible();
});

test("creates a collection and edits it", async ({ page }) => {
	const name = `Mine ${uniqueWord()}`;
	await page.goto("/account/collections");
	await page.getByRole("button", { name: "New collection" }).click();
	const dialog = page.getByRole("dialog", { name: "New collection" });
	await dialog.getByLabel("Name").fill(name);
	await dialog.getByLabel("Visibility").selectOption("Public");
	await dialog.getByRole("button", { name: "Create" }).click();
	await expect(
		collectionRows(page).getByRole("listitem").filter({ hasText: name }),
	).toContainText("Public");

	await page.reload();
	await collectionRows(page).getByRole("link", { name }).click();
	await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
	await expect(
		page.getByText("This collection is empty. Add entries from their pages."),
	).toBeVisible();
	await expect(page.getByText("Shareable link:")).toBeVisible();

	const renamed = `Renamed ${uniqueWord()}`;
	await page.getByRole("button", { name: "Edit collection" }).click();
	await page.getByLabel("Name").fill(renamed);
	await page.getByLabel("Description").fill("Things I **like**.");
	await page.getByLabel("Visibility").selectOption("Private");
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByRole("heading", { level: 1, name: renamed })).toBeVisible();
	await expect(page.locator("[data-collection] .description strong")).toHaveText("like");
	await expect(page.getByText("Shareable link:")).toBeHidden();

	await page.getByRole("link", { name: "All collections" }).click();
	await expect(
		collectionRows(page).getByRole("listitem").filter({ hasText: renamed }),
	).toContainText("Private");
});

test("deletes a collection but keeps its entries", async ({ page, seed, me }) => {
	const entry = await seed.entry();
	const collection = await seed.collection(me);
	await seed.addToCollection(me, collection, entry);

	await page.goto("/account/collections");
	page.once("dialog", (dialog) => void dialog.accept());
	await page.getByRole("button", { name: `Delete ${collection.name}` }).click();
	await expect(page.getByText("You have no collections yet.")).toBeVisible();
	await page.reload();
	await expect(page.getByText("You have no collections yet.")).toBeVisible();

	await page.goto(`/e/${entry.id}`);
	await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
});

test("removes an entry on the collection page", async ({ page, seed, me }) => {
	const kept = await seed.entry({ title: `Kept ${uniqueWord()}` });
	const dropped = await seed.entry({ title: `Dropped ${uniqueWord()}` });
	const collection = await seed.collection(me);
	await seed.addToCollection(me, collection, kept);
	await seed.addToCollection(me, collection, dropped);

	await page.goto(`/account/collections/${collection.id}`);
	await expect(entryRows(page).getByRole("listitem")).toHaveCount(2);
	await entryRows(page)
		.getByRole("listitem")
		.filter({ hasText: dropped.title })
		.getByRole("button", { name: "Remove" })
		.click();
	await expect(entryRows(page).getByRole("listitem")).toHaveCount(1);

	await page.reload();
	await expect(entryRows(page).getByRole("link", { name: kept.title })).toBeVisible();
	await expect(entryRows(page).getByRole("link", { name: dropped.title })).toBeHidden();
});

test("reorders the entries of a collection by keyboard", async ({ page, seed, me }) => {
	const first = await seed.entry({ title: `First ${uniqueWord()}` });
	const second = await seed.entry({ title: `Second ${uniqueWord()}` });
	const collection = await seed.collection(me);
	await seed.addToCollection(me, collection, first);
	await seed.addToCollection(me, collection, second);

	await page.goto(`/account/collections/${collection.id}`);
	const rows = entryRows(page).getByRole("listitem");
	await expect(rows.first()).toContainText(first.title);
	await rows.first().focus();
	const saved = page.waitForResponse(
		(response) => response.request().method() === "PUT" && response.url().endsWith("/entries"),
	);
	await page.keyboard.press("Space");
	await page.keyboard.press("ArrowDown");
	await page.keyboard.press("Space");
	await expect(rows.first()).toContainText(second.title);
	expect((await saved).status()).toBe(204);

	await page.reload();
	await expect(rows.first()).toContainText(second.title);
});

test("adds and removes an entry from the entry page", async ({ page, seed, me }) => {
	const entry = await seed.entry({ title: `Collected ${uniqueWord()}` });
	const collection = await seed.collection(me);

	await page.goto(`/e/${entry.id}`);
	let dialog = await openPicker(page);
	const membership = dialog.getByRole("checkbox", { name: new RegExp(`^${collection.name}`) });
	await expect(membership).not.toBeChecked();
	await membership.check();
	// Nothing is written until Save, which closes the dialog.
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog).toBeHidden();

	await page.goto(`/account/collections/${collection.id}`);
	await expect(entryRows(page).getByRole("link", { name: entry.title })).toBeVisible();

	await page.goto(`/e/${entry.id}`);
	dialog = await openPicker(page);
	await expect(membership).toBeChecked();
	await membership.uncheck();
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog).toBeHidden();

	await page.goto(`/account/collections/${collection.id}`);
	await expect(
		page.getByText("This collection is empty. Add entries from their pages."),
	).toBeVisible();
});

test("the picker shows which collections already hold the entry", async ({ page, seed, me }) => {
	const entry = await seed.entry({ title: `Held ${uniqueWord()}` });
	const holding = await seed.collection(me);
	const other = await seed.collection(me);
	await seed.addToCollection(me, holding, entry);
	await seed.collection(await seed.account()); // someone else's

	const membershipReads: string[] = [];
	page.on("request", (request) => {
		if (
			/\/api\/v1\/(collections\/[^/]+$|entries\/[^/]+\/collections$)/.test(
				new URL(request.url()).pathname,
			)
		) {
			membershipReads.push(new URL(request.url()).pathname);
		}
	});
	await page.goto(`/e/${entry.id}`);
	const dialog = await openPicker(page);
	await expect(dialog.getByRole("checkbox")).toHaveCount(2);
	await expect(
		dialog.getByRole("checkbox", { name: new RegExp(`^${holding.name}`) }),
	).toBeChecked();
	await expect(
		dialog.getByRole("checkbox", { name: new RegExp(`^${other.name}`) }),
	).not.toBeChecked();
	// One membership read, not one per collection.
	expect(membershipReads).toEqual([`/api/v1/entries/${entry.id}/collections`]);
});

test("creates a collection holding the entry from the entry page", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Quick ${uniqueWord()}` });
	const name = `Quick list ${uniqueWord()}`;

	await page.goto(`/e/${entry.id}`);
	let dialog = await openPicker(page);
	await expect(
		dialog.getByText("You have no collections yet. Create one with New collection."),
	).toBeVisible();
	await dialog.getByRole("button", { name: "New collection" }).click();
	await expect(dialog.getByRole("checkbox", { name: "Add to the new collection" })).toBeChecked();
	// A ticked new collection needs a name.
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog.getByText("Name the new collection, or untick it.")).toBeVisible();
	await dialog.getByRole("textbox", { name: "New collection name" }).fill(name);
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog).toBeHidden();
	dialog = await openPicker(page);
	await expect(dialog.getByRole("checkbox", { name: new RegExp(`^${name}`) })).toBeChecked();

	await page.goto("/account/collections");
	await collectionRows(page).getByRole("link", { name }).click();
	await expect(entryRows(page).getByRole("link", { name: entry.title })).toBeVisible();
});

test("adds an entry from a blueprint page inside a book", async ({ page, seed, me }) => {
	const entry = await seed.entry({ title: `Book entry ${uniqueWord()}`, blueprint: BOOK });
	const collection = await seed.collection(me);

	await page.goto(`/e/${entry.id}`);
	await (await bookLeaf(page, "Assembler 3 cell")).click();
	await expect(page.getByRole("heading", { level: 1, name: "Assembler 3 cell" })).toBeVisible();
	const dialog = await openPicker(page);
	await dialog.getByRole("checkbox", { name: new RegExp(`^${collection.name}`) }).check();
	await dialog.getByRole("button", { name: "Save" }).click();
	await expect(dialog).toBeHidden();

	await page.goto(`/account/collections/${collection.id}`);
	await expect(entryRows(page).getByRole("link", { name: entry.title })).toBeVisible();
});
