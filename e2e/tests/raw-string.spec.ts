import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BOOK, OTHER_BLUEPRINT, relabel } from "../support/blueprints.ts";
import { expect, test, viewers } from "../support/fixtures.ts";
import { bookLeaf, openExchangeString, pickRevision } from "../support/ui.ts";

// The raw string link: a plain-text copy of the exchange string that any site
// may fetch, and "Open in editor", which hands that link to the blueprint editor.
const rawLink = (page: Page) => page.getByRole("button", { name: "Raw string" });
const editorLink = (page: Page) => page.getByRole("button", { name: "Open in editor" });

/** Checks the editor link loads the given raw-string URL. */
async function expectEditorSource(page: Page, rawHref: string): Promise<void> {
	const href = await editorLink(page).getAttribute("href");
	expect(href).toBe(`https://fbe.factorygamefan.com/?source=${encodeURIComponent(rawHref)}`);
}

/** Fetches the link's target and checks it is the bare string as text. */
async function expectRawString(page: Page, want: string): Promise<string> {
	const href = await rawLink(page).getAttribute("href");
	expect(href).toBeTruthy();
	const response = await page.request.get(href!);
	expect(response.status()).toBe(200);
	expect(response.headers()["content-type"]).toBe("text/plain; charset=utf-8");
	expect(response.headers()["content-disposition"]).toMatch(/^inline; filename="[\w-]+\.txt"$/);
	expect(await response.text()).toBe(want);
	return href!;
}

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("a blueprint links its raw string", async ({ page, seed }) => {
			const blueprint = relabel(BLUEPRINT, `Raw ${uniqueWord()}`);
			const entry = await seed.entry({ blueprint });

			await page.goto(`/e/${entry.id}`);
			// The entry links the exact upload; the textbox shows the stored leaf.
			const href = await expectRawString(page, blueprint);
			// The live revision needs no revision id in the link.
			expect(new URL(href).pathname).toBe(`/api/v1/entries/${entry.id}/raw`);
			await expectEditorSource(page, href);
		});

		test("an older revision links its own raw string", async ({ page, seed }) => {
			const before = relabel(BLUEPRINT, `Before ${uniqueWord()}`);
			const entry = await seed.entry({ blueprint: before });
			await seed.revision(entry, relabel(OTHER_BLUEPRINT, `After ${uniqueWord()}`));

			await page.goto(`/e/${entry.id}`);
			await pickRevision(page, "Revision 1");
			await expect(page).toHaveURL(/[?&]revision=/);
			const href = await expectRawString(page, before);
			expect(new URL(href).pathname).toMatch(
				new RegExp(`^/api/v1/entries/${entry.id}/revisions/[^/]+/raw$`),
			);
			await expectEditorSource(page, href);
		});

		test("a book and its blueprints link their raw strings", async ({ page, seed }) => {
			const entry = await seed.entry({ blueprint: BOOK });

			await page.goto(`/e/${entry.id}`);
			const bookHref = await expectRawString(page, BOOK);
			await expectEditorSource(page, bookHref);

			await (await bookLeaf(page, "Assembler 2 cell")).click();
			await expect(
				page.getByRole("heading", { level: 1, name: "Assembler 2 cell" }),
			).toBeVisible();
			const leafString = await (await openExchangeString(page, "Blueprint")).inputValue();
			await expectEditorSource(page, await expectRawString(page, leafString));
		});
	});
}

test("another site can fetch a raw string", async ({ page, seed }) => {
	const blueprint = relabel(BLUEPRINT, `Shared ${uniqueWord()}`);
	const entry = await seed.entry({ blueprint });
	await page.goto(`/e/${entry.id}`);
	const href = await rawLink(page).getAttribute("href");

	// The site on another origin the API does not allow (127.0.0.1 instead of
	// localhost): only the open CORS of the raw routes lets it read the string.
	const foreign = new URL("/about", page.url());
	foreign.hostname = "127.0.0.1";
	await page.goto(foreign.toString());
	const fetched = await page.evaluate(async (url) => {
		const response = await fetch(url);
		return { status: response.status, body: await response.text() };
	}, href!);
	expect(fetched).toEqual({ status: 200, body: blueprint });

	// The rest of the API stays closed to that origin.
	const entryUrl = new URL(`/api/v1/entries/${entry.id}`, href!).toString();
	const closed = await page.evaluate(
		async (url) =>
			fetch(url).then(
				() => "read",
				() => "refused",
			),
		entryUrl,
	);
	expect(closed).toBe("refused");
});

test.describe("as a member", () => {
	test.use({ viewer: "member" });

	test("a private entry has no raw string or editor link", async ({ page, seed, me }) => {
		const entry = await seed.entry({
			title: `Private ${uniqueWord()}`,
			owner: me,
			visibility: "private",
		});

		await page.goto(`/e/${entry.id}`);
		await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
		await openExchangeString(page, "Blueprint");
		await expect(rawLink(page)).toBeHidden();
		await expect(editorLink(page)).toBeHidden();
	});
});
