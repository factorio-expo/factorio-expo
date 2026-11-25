// Similar blueprints and compare. The e2e stack runs no Factorio, so every
// analysis is stored straight in the database (`storeAnalysis`, which makes 0.5
// electronic circuits per second times `rate`) and the search reindexed
// (`reindexEntry`); the blueprints are unrelated to what they "make".
import type { APIRequestContext, Page } from "@playwright/test";
import { type SeededEntry, type Seeder, uniqueWord } from "../support/api.ts";
import { BLUEPRINT, relabel } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { apiURL, reindexEntry, storeAnalysis } from "../support/stack.ts";

type Analysed = SeededEntry & { leaf: string };

/** The id of the entry's one blueprint. */
async function leafOf(request: APIRequestContext, entry: SeededEntry): Promise<string> {
	const response = await request.get(`${apiURL}/api/v1/entries/${entry.id}`);
	const body = (await response.json()) as { revision: { root: { id: string } } };
	return body.revision.root.id;
}

async function analysed(
	seed: Seeder,
	request: APIRequestContext,
	title: string,
	result: Parameters<typeof storeAnalysis>[1],
	options: Parameters<Seeder["entry"]>[0] = {},
): Promise<Analysed> {
	// A string of its own: the API reuses the analysis of an identical one.
	const entry = await seed.entry({ title, blueprint: relabel(BLUEPRINT, title), ...options });
	storeAnalysis(entry.id, result);
	reindexEntry(entry.id);
	return { ...entry, leaf: await leafOf(options.owner?.api ?? request, entry) };
}

/** Waits until the index lists `expected` blueprints similar to `leaf`, closest first. */
async function similarTo(request: APIRequestContext, leaf: string, expected: string[]) {
	await expect
		.poll(
			async () => {
				const response = await request.get(`${apiURL}/api/v1/search`, {
					params: { similar_to: leaf, limit: expected.length },
				});
				const body = (await response.json()) as { results?: { title: string }[] };
				return body.results?.map((result) => result.title);
			},
			{ message: "the index never listed the similar blueprints", timeout: 30_000 },
		)
		.toEqual(expected);
}

const card = (page: Page, title: string) =>
	page.getByRole("article").filter({ has: page.getByRole("heading", { name: title }) });

test.describe("similar blueprints and compare", () => {
	test("lists similar blueprints closest first and compares two", async ({
		page,
		seed,
		request,
	}) => {
		const word = uniqueWord();
		// Rates 1000/s, 1200/s and 3000/s of the same product; a space platform
		// of nearly the same rate and one without an analysis never count.
		const base = await analysed(seed, request, `Base ${word}`, {
			width: 12,
			height: 7,
			rate: 2000,
		});
		const close = await analysed(seed, request, `Close ${word}`, {
			width: 20,
			height: 5,
			rate: 2400,
		});
		const far = await analysed(seed, request, `Far ${word}`, {
			width: 4,
			height: 4,
			rate: 6000,
		});
		const ship = await analysed(seed, request, `Ship ${word}`, {
			width: 8,
			height: 8,
			rate: 2020,
			platform: true,
		});
		const unmeasured = await seed.entry({
			title: `Unmeasured ${word}`,
			blueprint: relabel(BLUEPRINT, `Unmeasured ${word}`),
		});
		await similarTo(request, base.leaf, [close.title, far.title]);

		// The leaf page shows the strip, closest first, and links the full list.
		await page.goto(`/e/${base.id}`);
		const strip = page.getByRole("region", { name: "Similar blueprints" });
		await expect(strip.getByRole("article").first()).toContainText(close.title);
		await expect(strip.getByRole("article").nth(1)).toContainText(far.title);
		await expect(strip.getByRole("article").filter({ hasText: base.title })).toHaveCount(0);
		await expect(strip.getByRole("article").filter({ hasText: ship.title })).toHaveCount(0);
		await strip.getByRole("link", { name: /^(See all \d+|Open in browse)$/ }).click();

		// /browse?similar_to= lists the same, without a sort to pick.
		await expect(page).toHaveURL(new RegExp(`/browse\\?similar_to=${base.leaf}$`));
		await expect(page.locator("[data-similar-note]")).toBeVisible();
		await expect(page.getByLabel("Sort by")).toHaveCount(0);
		await expect(page.getByRole("article").first()).toContainText(close.title);
		await expect(page.getByRole("article").nth(1)).toContainText(far.title);
		await expect(card(page, base.title)).toHaveCount(0);
		await expect(card(page, ship.title)).toHaveCount(0);
		await expect(card(page, unmeasured.title)).toHaveCount(0);
		// The filters still work next to it.
		await page.getByRole("searchbox", { name: "Search blueprints" }).fill(`Far ${word}`);
		await page.getByRole("button", { name: "Search", exact: true }).click();
		await expect(page).toHaveURL(new RegExp(`similar_to=${base.leaf}`));
		await expect(page.getByRole("article")).toHaveCount(1);
		await expect(card(page, far.title)).toBeVisible();

		// Compare: the first press picks the blueprint ...
		await page.goto(`/e/${base.id}`);
		const compare = page.getByRole("button", { name: "Compare" });
		await expect(compare).toHaveAttribute("aria-pressed", "false");
		await compare.click();
		await expect(compare).toHaveAttribute("aria-pressed", "true");
		// ... pressing it again drops it, and the pick survives a reload ...
		await compare.click();
		await expect(compare).toHaveAttribute("aria-pressed", "false");
		await compare.click();
		await page.reload();
		await expect(page.getByRole("button", { name: "Compare" })).toHaveAttribute(
			"aria-pressed",
			"true",
		);

		// ... and on another blueprint it opens the shareable comparison.
		await page.goto(`/e/${close.id}`);
		await page.getByRole("button", { name: "Compare" }).click();
		await expect(page).toHaveURL(`/compare?a=${base.leaf}&b=${close.leaf}`);
		await expect(
			page.getByRole("heading", { level: 1, name: "Compare blueprints" }),
		).toBeVisible();
		for (const [key, entry] of [
			["A", base],
			["B", close],
		] as const) {
			const side = page.getByRole("region", { name: `Blueprint ${key}` });
			await expect(
				side.locator(`a[href="/e/${entry.id}/b/${entry.leaf}"]`).first(),
			).toHaveAttribute("href", `/e/${entry.id}/b/${entry.leaf}`);
		}
		// The pick is spent: the next Compare click starts fresh.
		expect(await page.evaluate(() => sessionStorage.getItem("expo:compare-leaf"))).toBeNull();
		await page.goBack();
		await expect(page.getByRole("button", { name: "Compare" })).toHaveAttribute(
			"aria-pressed",
			"false",
		);
		await page.goForward();
		// A page reached another way with the key still set clears it on load.
		await page.evaluate((id) => sessionStorage.setItem("expo:compare-leaf", id), base.leaf);
		await page.reload();
		expect(await page.evaluate(() => sessionStorage.getItem("expo:compare-leaf"))).toBeNull();
		await page.goto(`/e/${far.id}`);
		await expect(page.getByRole("button", { name: "Compare" })).toHaveAttribute(
			"aria-pressed",
			"false",
		);
		await page.goto(`/compare?a=${base.leaf}&b=${close.leaf}`);

		const row = (id: string) => page.locator(`[data-compare-row="${id}"]`);
		// Equal and text figures carry no emphasis; a larger one is marked for screen readers.
		await expect(row("footprint").locator("td")).toHaveText(["12 × 7 tiles", "20 × 5 tiles"]);
		await expect(row("area").locator("td")).toHaveText([
			"84 tiles (lower)",
			"100 tiles (higher)",
		]);
		await expect(row("area").locator("td.larger")).toHaveText("100 tiles (higher)");
		await expect(row("area").locator("td.smaller")).toHaveText("84 tiles (lower)");
		await expect(row("consumption").locator("td")).toHaveText(["90 kW", "90 kW"]);
		await expect(row("consumption").locator("td.larger, td.smaller")).toHaveCount(0);
		const products = page
			.locator('[data-compare-group="products"] [data-compare-row]')
			.first()
			.locator("td");
		await expect(products).toHaveText(["1,000/s (lower)", "1,200/s (higher)"]);
		// Neutral emphasis by weight, not colour alone: the larger is bold, the smaller plain.
		const weight = (cell: typeof products) =>
			cell.evaluate((el) => Number(getComputedStyle(el).fontWeight));
		expect(await weight(products.nth(1))).toBeGreaterThanOrEqual(700);
		expect(await weight(products.nth(0))).toBeLessThan(600);
		// Compact on desktop: a narrow table, not stretched over the page.
		const table = await page.locator("[data-compare-table]").boundingBox();
		expect(table!.width).toBeLessThanOrEqual(740);

		// Swapping flips the pair.
		await page.getByRole("link", { name: "Swap A and B" }).click();
		await expect(page).toHaveURL(`/compare?a=${close.leaf}&b=${base.leaf}`);
		await expect(row("footprint").locator("td")).toHaveText(["20 × 5 tiles", "12 × 7 tiles"]);
		await expect(row("area").locator("td")).toHaveText([
			"100 tiles (higher)",
			"84 tiles (lower)",
		]);

		// It fits a phone: no sideways scroll, the previews stacked, every row readable.
		await page.setViewportSize({ width: 375, height: 800 });
		await expect(row("footprint").locator("td").first()).toBeVisible();
		expect(
			await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
		).toBe(true);
		const [a, b] = await Promise.all([
			page.getByRole("region", { name: "Blueprint A" }).boundingBox(),
			page.getByRole("region", { name: "Blueprint B" }).boundingBox(),
		]);
		expect(b!.y).toBeGreaterThan(a!.y + a!.height - 1);
		// Each metric stacks: its label on its own line above the A and B figures.
		const label = await row("area").locator("th").boundingBox();
		const [cellA, cellB] = await Promise.all(
			[0, 1].map((index) => row("area").locator("td").nth(index).boundingBox()),
		);
		expect(cellA!.y).toBeGreaterThan(label!.y + label!.height - 1);
		expect(Math.abs(cellA!.y - cellB!.y)).toBeLessThan(2);
		expect(cellB!.x).toBeGreaterThan(cellA!.x);
		// Wide, they sit side by side.
		await page.setViewportSize({ width: 1200, height: 800 });
		const [wideA, wideB] = await Promise.all([
			page.getByRole("region", { name: "Blueprint A" }).boundingBox(),
			page.getByRole("region", { name: "Blueprint B" }).boundingBox(),
		]);
		expect(wideB!.x).toBeGreaterThan(wideA!.x + wideA!.width - 1);
		expect(Math.abs(wideB!.y - wideA!.y)).toBeLessThan(2);
	});

	test("compares only public blueprints", async ({ page, seed, request }) => {
		const word = uniqueWord();
		const owner = await seed.account();
		const open = await analysed(seed, request, `Open ${word}`, { width: 3, height: 3 });
		const hidden = await analysed(
			seed,
			request,
			`Hidden ${word}`,
			{ width: 3, height: 3 },
			{ owner, visibility: "private" },
		);
		const unlisted = await analysed(
			seed,
			request,
			`Unlisted ${word}`,
			{ width: 3, height: 3 },
			{ owner, visibility: "unlisted" },
		);

		for (const other of [hidden.leaf, unlisted.leaf, "abcdefghijkl", "not-an-id"]) {
			const response = await page.goto(`/compare?a=${open.leaf}&b=${other}`);
			expect(response?.status()).toBe(404);
			expect(
				(await request.get(`${apiURL}/api/v1/compare?a=${open.leaf}&b=${other}`)).status(),
			).toBe(404);
		}
		for (const query of ["", `?a=${open.leaf}`, `?a=${open.leaf}&b=${open.leaf}`]) {
			expect((await page.goto(`/compare${query}`))?.status()).toBe(404);
		}
		// Similar to a blueprint that is not public is just as unknown.
		expect(
			(await request.get(`${apiURL}/api/v1/search?similar_to=${hidden.leaf}`)).status(),
		).toBe(404);
	});
});

test.describe("as the owner of a private blueprint", () => {
	test.use({ viewer: "member" });

	test("has no compare button or similar blueprints on it", async ({
		page,
		seed,
		request,
		me,
	}) => {
		const word = uniqueWord();
		const hidden = await analysed(
			seed,
			request,
			`Mine ${word}`,
			{ width: 3, height: 3 },
			{ owner: me, visibility: "private" },
		);
		await page.goto(`/e/${hidden.id}`);
		await expect(page.getByRole("heading", { level: 1, name: hidden.title })).toBeVisible();
		await expect(page.getByRole("button", { name: "Compare" })).toHaveCount(0);
		await expect(page.getByRole("region", { name: "Similar blueprints" })).toHaveCount(0);
	});
});
