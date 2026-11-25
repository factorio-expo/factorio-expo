// The landing page: search into /browse, the featured challenge, staff picks,
// trending (windowed views), the leaderboard's freshest records and the
// newest uploads.
import type { Browser, Page } from "@playwright/test";
import { uniqueWord, type SeededEntry } from "../support/api.ts";
import { expect, test } from "../support/fixtures.ts";
import { apiURL, createChallenge, pickForHome } from "../support/stack.ts";
import { ready } from "../support/ui.ts";

// A bot user agent is not counted; each visitor is one agent (one view a day).
const visitorAgent = (name: string) =>
	`Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 ${name}`;

/** Opens the entry as a separate visitor and waits for the view beacon's answer. */
async function visitAs(browser: Browser, name: string, entry: SeededEntry): Promise<void> {
	const context = await browser.newContext({ userAgent: visitorAgent(name) });
	const page = await context.newPage();
	const beacon = page.waitForResponse(
		(response) => response.request().method() === "POST" && response.url().endsWith("/view"),
	);
	await page.goto(`/e/${entry.id}`);
	await beacon;
	await context.close();
}

const card = (page: Page, title: string) =>
	page.getByRole("article").filter({ has: page.getByRole("heading", { name: title }) });

test("the search box opens the browse page with the query", async ({ page, seed }) => {
	const word = uniqueWord();
	const entry = await seed.entry({ title: `Home ${word}` });
	await seed.searchable(entry, word);

	await page.goto("/");
	await expect(page.getByRole("heading", { level: 1 })).toHaveText("Factorio Expo");
	await page.getByRole("searchbox", { name: "Search blueprints" }).fill(word);
	await page.getByRole("button", { name: "Search", exact: true }).click();
	await expect(page).toHaveURL(new RegExp(`/browse[?]q=${word}`));
	await expect(card(page, entry.title)).toBeVisible();
});

test("the sidebar's Browse goes to /browse and Home back", async ({ page }) => {
	await page.goto("/");
	const sidebar = page.getByRole("navigation", { name: "Sidebar" });
	await sidebar.getByRole("link", { name: "Browse" }).click();
	await expect(page).toHaveURL("/browse");
	await expect(page.getByRole("heading", { level: 1, name: "Browse entries" })).toBeVisible();
	await sidebar.getByRole("link", { name: "Home" }).click();
	await expect(page).toHaveURL("/");
});

test("lists the newest uploads and links the browse page sorted by date", async ({
	page,
	seed,
}) => {
	const entry = await seed.entry({ title: `Newest ${uniqueWord()}` });
	await seed.searchable(entry);

	await page.goto("/");
	const section = page.getByRole("region", { name: "Newest uploads" });
	await expect(
		section
			.getByRole("article")
			.filter({ has: page.getByRole("heading", { name: entry.title }) }),
	).toBeVisible();
	await section.getByRole("link", { name: "Browse all" }).click();
	await expect(page).toHaveURL(/\/browse\?sort=created_at(:|%3A)desc/);
});

test("trending ranks the entries viewed in the period", async ({
	page,
	browser,
	seed,
	request,
}) => {
	const entry = await seed.entry({ title: `Trending ${uniqueWord()}` });
	for (const name of ["one", "two", "three"])
		await visitAs(browser, `${name}-${entry.id}`, entry);
	await expect
		.poll(
			async () => {
				const response = await request.get(`${apiURL}/api/v1/trending`, {
					params: { period: "day" },
				});
				const body = (await response.json()) as { entries: { id: string }[] };
				return body.entries.some((item) => item.id === entry.id);
			},
			{ message: "the views never reached today's trending", timeout: 20_000 },
		)
		.toBe(true);

	await page.goto("/");
	const section = page.getByRole("region", { name: "Trending" });
	const tab = (name: string) => section.getByRole("tab", { name });
	const panel = (name: string) => section.getByRole("tabpanel", { name });
	const inPanel = (name: string) =>
		panel(name)
			.getByRole("article")
			.filter({ has: page.getByRole("heading", { name: entry.title }) });
	await ready(page);

	// The week opens, or the month while the week is thin; every panel is already
	// on the page, so switching asks the API for nothing.
	await expect(section.getByRole("tab", { selected: true })).toHaveCount(1);
	const requests: string[] = [];
	page.on("request", (sent) => requests.push(sent.url()));
	await tab("Today").click();
	await expect(tab("Today")).toHaveAttribute("aria-selected", "true");
	await expect(inPanel("Today")).toBeVisible();
	await tab("This month").click();
	await expect(inPanel("This month")).toBeVisible();
	await expect(panel("Today")).toBeHidden();
	await tab("This month").press("ArrowLeft");
	await expect(tab("This week")).toBeFocused();
	await expect(tab("This week")).toHaveAttribute("aria-selected", "true");
	expect(requests.filter((url) => url.includes("/api/v1/"))).toEqual([]);
});

test("the advanced search link opens the browse page with the filters showing", async ({
	page,
}) => {
	await page.goto("/");
	await page.getByRole("link", { name: "Advanced search" }).click();
	await expect(page).toHaveURL("/browse?advanced=1");
	await expect(page.getByRole("heading", { level: 1, name: "Browse entries" })).toBeVisible();
	await expect(page.locator("details.advanced-search")).toHaveAttribute("open", "");
	await expect(page.locator('meta[name="robots"]')).toHaveAttribute(
		"content",
		"noindex, nofollow",
	);
});

test("the home page has one h1, sections as h2 and cards as h3", async ({ page, seed }) => {
	await seed.searchable(await seed.entry({ title: `Outline ${uniqueWord()}` }));
	await page.goto("/");
	await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
	await expect(page.getByRole("heading", { level: 2 }).first()).toBeVisible();
	await expect(
		page
			.getByRole("region", { name: "Newest uploads" })
			.getByRole("heading", { level: 3 })
			.first(),
	).toBeVisible();
	await expect(page.getByRole("article").getByRole("heading", { level: 2 })).toHaveCount(0);
});

test("the trending API refuses an unknown period", async ({ request }) => {
	const response = await request.get(`${apiURL}/api/v1/trending`, { params: { period: "year" } });
	expect(response.status()).toBe(422);
});

test("features an open challenge", async ({ page }) => {
	const word = uniqueWord();
	const slug = word.toLowerCase().replaceAll(/[^a-z0-9]+/g, "-");
	createChallenge(slug, `Spotlight ${word.replaceAll(/[^A-Za-z0-9]+/g, " ")}`);

	await page.goto("/");
	const spotlight = page.getByRole("region", { name: /./, exact: false }).filter({
		hasText: "Open challenge",
	});
	await expect(spotlight).toBeVisible();
	await spotlight.getByRole("button", { name: "Enter the challenge" }).click();
	await expect(page).toHaveURL(/\/challenges\/[a-z0-9-]+$/);
});

test("the home page is indexed with its own canonical address", async ({ page }) => {
	await page.goto("/");
	await expect(page.locator('meta[name="robots"]')).toHaveCount(0);
	await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
		"href",
		/^https?:\/\/[^/]+\/$/,
	);
	expect(await page.locator('script[type="application/ld+json"]').textContent()).toContain(
		"SearchAction",
	);
});

test("shows the staff picks with their caption until the entry leaves public", async ({
	page,
	seed,
}) => {
	const owner = await seed.account();
	const entry = await seed.entry({ title: `Picked ${uniqueWord()}`, owner });
	pickForHome(entry.id, "Clean ratios, no beaconing");

	await page.goto("/");
	const picks = page.getByRole("region", { name: "Staff picks" });
	const pick = picks.locator("[data-noted-card]").filter({ hasText: entry.title });
	// The caption sits where a book's title goes on a card, in the same look.
	const caption = pick.locator("[data-card-caption]");
	await expect(caption).toContainText("Staff pick");
	await expect(caption.locator("svg")).toHaveCount(0);
	await expect(caption).toHaveClass(/\bbook\b/);
	await expect(caption.locator(".book-title")).toHaveText("Clean ratios, no beaconing");
	await pick.getByRole("link", { name: entry.title }).first().click();
	await expect(page).toHaveURL(new RegExp(`/e/${entry.id}`));

	const response = await owner.api.patch(`/api/v1/entries/${entry.id}`, {
		data: { visibility: "private" },
	});
	expect(response.ok()).toBe(true);
	await page.goto("/");
	await expect(page.getByRole("region", { name: "Staff picks" })).toHaveCount(0);
});
