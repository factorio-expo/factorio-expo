import type { APIRequestContext, Browser, Page } from "@playwright/test";
import { uniqueWord, type SeededEntry } from "../support/api.ts";
import { BOOK } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { bookLeaf } from "../support/ui.ts";
import { apiURL } from "../support/stack.ts";

// Headless Chromium names itself HeadlessChrome, which the API skips as a
// bot; visitors here present a plain desktop Chrome user agent. A distinct
// agent is a distinct visitor (one view per visitor per entry per day).
const visitorAgent = (name: string) =>
	`Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 ${name}`;

test.use({ userAgent: visitorAgent("visitor") });

const viewCount = async (request: APIRequestContext, entry: SeededEntry) => {
	const response = await request.get(`${apiURL}/api/v1/entries/${entry.id}`);
	return ((await response.json()) as { view_count: number }).view_count;
};

/** Opens a page and waits until the API answered its view beacon (closing the
 * context earlier could cancel the request). */
async function visit(page: Page, path: string): Promise<void> {
	const beacon = page.waitForResponse(
		(response) => response.request().method() === "POST" && response.url().endsWith("/view"),
	);
	await page.goto(path);
	await beacon;
}

/** Opens the entry as a separate visitor (its own browser context). */
async function visitAs(browser: Browser, name: string, entry: SeededEntry): Promise<void> {
	const context = await browser.newContext({ userAgent: visitorAgent(name) });
	await visit(await context.newPage(), `/e/${entry.id}`);
	await context.close();
}

test("counts one view per visitor and shows it on the entry", async ({ page, seed, request }) => {
	const entry = await seed.entry({ title: `Viewed ${uniqueWord()}`, blueprint: BOOK });

	await visit(page, `/e/${entry.id}`);
	await expect
		.poll(() => viewCount(request, entry), { message: "the view was never counted" })
		.toBe(1);

	// Reloading, and opening a blueprint of the same entry, is the same visitor.
	await visit(page, `/e/${entry.id}`);
	await (await bookLeaf(page, "Assembler 2 cell")).click();
	await expect(page.locator("[data-view-beacon]")).toBeAttached();
	// Two flush intervals pass without another view being added.
	await page.waitForTimeout(2_500);
	expect(await viewCount(request, entry)).toBe(1);

	await page.goto(`/e/${entry.id}`);
	await expect(page.locator("[data-entry-views]")).toHaveText("1");
});

test("a bot's visit is not counted", async ({ browser, seed, request }) => {
	const entry = await seed.entry({ title: `Crawled ${uniqueWord()}` });
	const context = await browser.newContext({
		userAgent: "Mozilla/5.0 (compatible; Googlebot/2.1)",
	});
	await visit(await context.newPage(), `/e/${entry.id}`);
	await context.close();

	// A visitor's view lands; the bot's, sent before it, never does.
	await visitAs(browser, "later-visitor", entry);
	await expect.poll(() => viewCount(request, entry)).toBe(1);
	await new Promise((resolve) => setTimeout(resolve, 2_500));
	expect(await viewCount(request, entry)).toBe(1);
});

test.describe("as the owner", () => {
	test.use({ viewer: "member" });

	test("an owner's visits are not counted", async ({ page, seed, me }) => {
		const entry = await seed.entry({ title: `Mine ${uniqueWord()}`, owner: me });

		await page.goto(`/e/${entry.id}`);
		await expect(page.locator("[data-entry-views]")).toHaveText("0");
		await expect(page.locator("[data-view-beacon]")).toHaveCount(0);
	});
});

test("an edit-link holder's visits are not counted", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Anonymous ${uniqueWord()}` });

	await page.goto(`/e/${entry.id}?token=${entry.editToken}`);
	await expect(page.getByRole("button", { name: "Delete entry" })).toBeVisible();
	await expect(page.locator("[data-view-beacon]")).toHaveCount(0);
});

test("search sorts by views and cards show them", async ({ page, browser, seed, request }) => {
	const word = uniqueWord();
	const quiet = await seed.entry({ title: `Quiet ${word}` });
	const popular = await seed.entry({ title: `Popular ${word}` });
	await seed.searchable(quiet, word);
	await seed.searchable(popular, word);

	await visitAs(browser, "first", popular);
	await visitAs(browser, "second", popular);
	await visitAs(browser, "first", quiet);
	await expect.poll(() => viewCount(request, popular)).toBe(2);

	// The search index picks up the new counts on its next refresh.
	await expect
		.poll(
			async () => {
				const response = await request.get(`${apiURL}/api/v1/search`, {
					params: { q: word, sort: "view_count:desc" },
				});
				const body = (await response.json()) as {
					results: { entry_id: string; view_count: number }[];
				};
				return body.results.map((result) => `${result.entry_id}:${result.view_count}`);
			},
			{ message: "search never sorted by the new view counts", timeout: 20_000 },
		)
		.toEqual([`${popular.id}:2`, `${quiet.id}:1`]);

	await page.goto(`/browse?q=${word}`);
	// Choosing a sort submits the form.
	await page.getByLabel("Sort by").selectOption("view_count:desc");
	await expect(page).toHaveURL(/sort=view_count%3Adesc/);
	const titles = page.getByRole("article").getByRole("heading");
	await expect(titles).toHaveText([popular.title, quiet.title]);
	await expect(page.getByRole("article").first().locator("[data-card-views]")).toHaveText(
		/2\s*views/,
	);
});
