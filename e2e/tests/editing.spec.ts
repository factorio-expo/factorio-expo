// Editing entry details, adding revisions and deleting entries, by the two kinds of editor: a
// guest holding the edit link of an anonymous upload, and a signed-in owner.
import { uniqueWord, type Account, type Seeder, type SeededEntry } from "../support/api.ts";
import { BLUEPRINT, BOOK, OTHER_BLUEPRINT, relabel } from "../support/blueprints.ts";
import { expect, test, type Viewer } from "../support/fixtures.ts";
import { webURL } from "../support/stack.ts";
import {
	editDescription,
	editTitle,
	holdScripts,
	revisionButton,
	submitRevision,
} from "../support/ui.ts";

const editors: { name: string; viewer: Viewer }[] = [
	{ name: "with the edit link", viewer: "guest" },
	{ name: "as the owner", viewer: "member" },
];

const kinds = [
	{
		kind: "blueprint",
		exchange: BLUEPRINT,
		next: (label: string) => relabel(OTHER_BLUEPRINT, label),
	},
	{ kind: "book", exchange: BOOK, next: (label: string) => relabel(BOOK, label) },
] as const;

/** An entry this editor may change, and the URL that opens it as them. */
async function editableEntry(seed: Seeder, owner: Account | undefined, blueprint: string) {
	const entry: SeededEntry = await seed.entry({ blueprint, owner });
	const url = entry.editToken ? `/e/${entry.id}?token=${entry.editToken}` : `/e/${entry.id}`;
	return { entry, url };
}

for (const editor of editors) {
	test.describe(editor.name, () => {
		test.use({ viewer: editor.viewer });

		for (const { kind, exchange, next } of kinds) {
			test(`edits the title and description of a ${kind}`, async ({
				page,
				seed,
				account,
				browser,
			}) => {
				const { entry, url } = await editableEntry(seed, account, exchange);
				const title = `Renamed ${uniqueWord()}`;

				await page.goto(url);
				await editTitle(page, title);
				await editDescription(
					page,
					"Now with a **description**.",
					"Now with a description.",
				);

				// Saved for everyone, not just this browser.
				const visitor = await browser.newPage({ baseURL: webURL });
				await visitor.goto(`/e/${entry.id}`);
				await expect(visitor.getByRole("heading", { level: 1, name: title })).toBeVisible();
				await expect(visitor.locator(".entry-description strong")).toHaveText(
					"description",
				);
				await visitor.close();
			});

			test(`adds a revision to a ${kind}`, async ({ page, seed, account }) => {
				const { entry, url } = await editableEntry(seed, account, exchange);
				const label = `Revised ${uniqueWord()}`;

				await page.goto(url);
				await expect(revisionButton(page)).toHaveText("Revision 1");
				await page.getByRole("button", { name: "New revision" }).click();
				// The form starts from the current exchange string.
				await expect(page.getByLabel("Blueprint exchange string")).toHaveValue(exchange);
				await expect(page.getByLabel("Entry title")).toHaveValue(entry.title);
				await submitRevision(page, next(label));

				await expect(
					page.getByRole("heading", { level: 1, name: entry.title }),
				).toBeVisible();
				await expect(revisionButton(page)).toHaveText("Revision 2");
				await expect(page.getByText(label)).toBeVisible();
				// Still an editor afterwards (the edit link survives the round trip).
				await expect(page.getByRole("button", { name: "Edit details" })).toBeVisible();
			});
		}

		test("cancelling an edit keeps the old title", async ({ page, seed, account }) => {
			const { entry, url } = await editableEntry(seed, account, BLUEPRINT);
			await page.goto(url);
			await page.getByRole("button", { name: "Edit details" }).click();
			await page.getByLabel("Entry title").fill("Never saved");
			await page.getByRole("button", { name: "Cancel" }).click();
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
			await page.reload();
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
		});

		test("deletes the entry after confirming", async ({ page, seed, account, browser }) => {
			const { entry, url } = await editableEntry(seed, account, BLUEPRINT);
			await page.goto(url);
			page.once("dialog", (dialog) => void dialog.accept());
			await page.getByRole("button", { name: "Delete entry" }).click();

			// Owners land on their own list, guests on the front page.
			if (account) {
				await expect(page).toHaveURL("/account/entries");
				await expect(
					page.getByRole("heading", { level: 1, name: "My entries" }),
				).toBeVisible();
				await expect(page.getByText("You have not uploaded anything yet.")).toBeVisible();
			} else {
				await expect(page).toHaveURL("/");
			}

			// Gone for everyone.
			const visitor = await browser.newPage({ baseURL: webURL });
			const response = await visitor.goto(`/e/${entry.id}`);
			expect(response?.status()).toBe(404);
			await visitor.close();
		});

		test("dismissing the delete confirmation keeps the entry", async ({
			page,
			seed,
			account,
		}) => {
			const { entry, url } = await editableEntry(seed, account, BLUEPRINT);
			await page.goto(url);
			page.once("dialog", (dialog) => void dialog.dismiss());
			await page.getByRole("button", { name: "Delete entry" }).click();
			await page.reload();
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
		});
	});
}

test.describe("without edit rights", () => {
	test("a wrong edit token shows no edit controls", async ({ page, seed }) => {
		const entry = await seed.entry();
		await page.goto(`/e/${entry.id}?token=not-the-token`);
		await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
		await expect(page.getByRole("button", { name: "Edit details" })).toBeHidden();
		await expect(page.getByRole("button", { name: "New revision" })).toBeHidden();
		await expect(page.getByRole("button", { name: "Delete entry" })).toBeHidden();
	});

	test("the new-revision page refuses a visitor without the edit link", async ({
		page,
		seed,
	}) => {
		const entry = await seed.entry();
		await page.goto(`/e/${entry.id}/revisions/new`);
		await expect(
			page.getByText(
				"Only the entry owner or a valid edit-token holder can create a revision.",
			),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "Create revision" })).toBeDisabled();
	});
});

test.describe("on a slow connection", () => {
	test("adding a revision before the page's scripts arrive keeps the edit link", async ({
		page,
		seed,
	}) => {
		const entry = await seed.entry();
		const release = await holdScripts(page);
		await page.goto(`/e/${entry.id}?token=${entry.editToken}`, { waitUntil: "commit" });

		// A plain link: it carries the edit token without any script.
		await page.getByRole("button", { name: "New revision" }).click();
		await expect(page).toHaveURL(`/e/${entry.id}/revisions/new?token=${entry.editToken}`);
		await page.getByLabel("Blueprint exchange string").fill(OTHER_BLUEPRINT);
		// Submitting now would bypass the script; the button waits for it.
		const submit = page.getByRole("button", { name: "Create revision" });
		await expect(submit).toBeDisabled();

		release();
		await submit.click();
		await expect(page).toHaveURL(`/e/${entry.id}?token=${entry.editToken}`);
		await expect(revisionButton(page)).toHaveText("Revision 2");
	});
});
