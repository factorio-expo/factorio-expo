import { uniqueWord } from "../support/api.ts";
import {
	BLUEPRINT,
	DECONSTRUCTION_BOOK,
	UPGRADE_BOOK,
	plannerFrom,
	withAbsoluteGrid,
} from "../support/blueprints.ts";
import { expect, test, viewers } from "../support/fixtures.ts";
import { openExchangeString } from "../support/ui.ts";

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("lists a book's deconstruction planners with their filters", async ({
			page,
			seed,
		}) => {
			const entry = await seed.entry({
				title: `Planners ${uniqueWord()}`,
				blueprint: DECONSTRUCTION_BOOK,
			});

			await page.goto(`/e/${entry.id}`);
			const cliffs = page.locator("details.planner").filter({ hasText: "Cliffs" }).first();
			await cliffs.locator(":scope > summary").click();
			await expect(cliffs).toContainText("Only the listed entities");
			await expect(cliffs).toContainText("Never removes tiles");
			// A special filter is named, not shown as its prototype name.
			await expect(cliffs.getByRole("list", { name: "Special filters" })).toContainText(
				"Cliffs",
			);
			// Copyable, never offered to the blueprint editor (only the book is).
			await cliffs.getByText("Deconstruction planner string").click();
			await expect(
				cliffs.getByRole("textbox", { name: "Deconstruction planner exchange string" }),
			).toHaveValue(/^0e/);
			await expect(page.getByRole("button", { name: "Open in editor" })).toHaveCount(1);

			const blacklist = page
				.locator("details.planner")
				.filter({ has: page.getByText("Everything but the grid", { exact: true }) });
			await blacklist.locator(":scope > summary").click();
			await expect(blacklist).toContainText("All entities except those listed");
			await expect(
				blacklist.getByRole("heading", { name: "Excluded entities" }),
			).toBeVisible();
			await expect(
				blacklist.getByRole("img", { name: "roboport", exact: true }),
			).toBeVisible();
		});

		test("lists an upgrade planner's from and to pairs", async ({ page, seed }) => {
			const entry = await seed.entry({
				title: `Upgrades ${uniqueWord()}`,
				blueprint: UPGRADE_BOOK,
			});

			await page.goto(`/e/${entry.id}`);
			const toUncommon = page
				.locator("details.planner")
				.filter({ hasText: "Below uncommon to uncommon" })
				.first();
			await toUncommon.locator(":scope > summary").click();
			const first = toUncommon
				.getByRole("list", { name: "Upgrades" })
				.getByRole("listitem")
				.first();
			await expect(
				first.getByRole("img", { name: "iron-chest, quality < uncommon" }),
			).toBeVisible();
			await expect(first.getByRole("img", { name: "iron-chest, uncommon" })).toBeVisible();
		});

		test("a planner on its own is an entry", async ({ page, seed }) => {
			const entry = await seed.entry({
				title: `Space Platform ${uniqueWord()}`,
				blueprint: plannerFrom(DECONSTRUCTION_BOOK, "Space platform"),
			});

			await page.goto(`/e/${entry.id}`);
			await expect(page.getByText("Removes only tiles")).toBeVisible();
			await expect(page.getByRole("heading", { name: "Tiles" })).toBeVisible();
			await expect(
				page.getByRole("img", { name: "space-platform-foundation", exact: true }),
			).toBeVisible();
			await expect(await openExchangeString(page, "Deconstruction planner")).toHaveValue(
				/^0e/,
			);
			await expect(page.getByRole("button", { name: "Copy string" })).toBeVisible();
			await expect(page.getByRole("button", { name: "Raw string" })).toBeVisible();
			await expect(page.getByRole("button", { name: "Open in editor" })).toHaveCount(0);
		});
	});
}

test("shows a blueprint's snap-to-grid setting", async ({ page, seed }) => {
	const entry = await seed.entry({
		title: `Grid ${uniqueWord()}`,
		blueprint: withAbsoluteGrid(BLUEPRINT, { x: 32, y: 16 }, { x: 3, y: 7 }),
	});

	await page.goto(`/e/${entry.id}`);
	await expect(page.locator("[data-snap-to-grid]")).toHaveText(
		"32 × 16 tiles, absolute (offset 3, 7)",
	);
});
