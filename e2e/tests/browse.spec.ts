import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import {
	BLUEPRINT,
	BLUEPRINT_21,
	blueprintOf,
	bookOf,
	DECONSTRUCTION_BOOK,
	plannerFrom,
	relabelPlanner,
	SPACE_AGE_20,
} from "../support/blueprints.ts";
import { expect, test, viewers } from "../support/fixtures.ts";
import { apiURL, reindexEntry, setEntryCreatedAt, storeAnalysis } from "../support/stack.ts";

const card = (page: Page, title: string) =>
	page.getByRole("article").filter({ has: page.getByRole("heading", { name: title }) });

async function search(page: Page, query: string) {
	await page.getByRole("searchbox", { name: "Search blueprints" }).fill(query);
	await page.getByRole("button", { name: "Search", exact: true }).click();
	await expect(page).toHaveURL(new RegExp(`[?&]q=${query}`));
}

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("lists entries on the home page", async ({ page, seed }) => {
			const entry = await seed.entry({ title: `Browse ${uniqueWord()}` });
			await seed.searchable(entry);

			await page.goto("/browse");
			await expect(page.getByText(/^\s*\d+ results?/)).toBeVisible();
			await expect(page.getByRole("article").first()).toBeVisible();
		});

		test("finds entries by text and opens one", async ({ page, seed }) => {
			const word = uniqueWord();
			const first = await seed.entry({
				title: `First ${word}`,
				description: "A *short* note",
			});
			const second = await seed.entry({ title: `Second ${word}` });
			await seed.entry({ title: `Unrelated ${uniqueWord()}` });
			await seed.searchable(first, word);
			await seed.searchable(second, word);

			await page.goto("/browse");
			await search(page, word);
			await expect(page.getByText("2 results")).toBeVisible();
			await expect(page.getByRole("article")).toHaveCount(2);
			await expect(card(page, first.title)).toContainText("A short note");
			await expect(card(page, first.title)).toContainText("Anonymous");

			await card(page, second.title).getByRole("link", { name: second.title }).click();
			await expect(page).toHaveURL(`/e/${second.id}`);
			await expect(page.getByRole("heading", { level: 1, name: second.title })).toBeVisible();
		});

		test("filters by tag", async ({ page, seed }) => {
			const word = uniqueWord();
			const tagged = await seed.entry({ title: `Rails ${word}`, tags: ["rail-grid"] });
			const untagged = await seed.entry({ title: `Plain ${word}` });
			await seed.searchable(tagged, word);
			await seed.searchable(untagged, word);

			await page.goto(`/browse?q=${word}`);
			await expect(page.getByRole("article")).toHaveCount(2);
			await page.getByText("Advanced search").click();
			await page.getByText("Any tag").click();
			// Tags are grouped, and the filter box matches tag and group names.
			const filter = page.getByLabel("Filter tags");
			await filter.fill("logistics");
			await expect(page.getByRole("checkbox", { name: "Rail grid" })).toBeVisible();
			await expect(page.getByRole("checkbox", { name: "Steam power" })).toBeHidden();
			await filter.fill("grid");
			await expect(page.getByRole("checkbox", { name: "Lane balancer" })).toBeHidden();
			await filter.fill("zzz");
			await expect(
				page.getByRole("listitem").filter({ hasText: "No matches" }),
			).toBeVisible();
			await filter.fill("rail");
			await page.getByRole("checkbox", { name: "Rail grid" }).check();
			await page.keyboard.press("Escape");
			await page.getByRole("button", { name: "Search", exact: true }).click();

			await expect(page).toHaveURL(/tags=rail-grid/);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(card(page, tagged.title)).toBeVisible();
		});

		test("filters by the mods a blueprint needs", async ({ page, seed }) => {
			const word = uniqueWord();
			const vanilla = await seed.entry({
				title: `Belts ${word}`,
				blueprint: blueprintOf("Belts", ["transport-belt", "inserter"]),
			});
			// Recyclers and fish breeding: Space Age content.
			const spaceAge = await seed.entry({ title: `Fish ${word}`, blueprint: SPACE_AGE_20 });
			const loaders = await seed.entry({
				title: `Loaders ${word}`,
				blueprint: blueprintOf("Loaders", ["transport-belt", "aai-loader"]),
			});
			for (const entry of [vanilla, spaceAge, loaders]) await seed.searchable(entry, word);

			await page.goto(`/browse?q=${word}`);
			await expect(page.getByRole("article")).toHaveCount(3);
			await page.getByText("Advanced search").click();
			await page.getByText("Any mods").click();
			await page.getByRole("checkbox", { name: "Vanilla only" }).check();
			await page.keyboard.press("Escape");
			await page.getByRole("button", { name: "Search", exact: true }).click();
			await expect(page).toHaveURL(/mods=vanilla/);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(card(page, vanilla.title)).toBeVisible();

			// Several choices match any of them.
			await page.locator("#search-mods-value").click();
			await page.getByLabel("Filter mods").fill("AAI Loaders");
			await page.getByRole("checkbox", { name: "AAI Loaders" }).check();
			await page.getByLabel("Filter mods").fill("Space");
			await page.getByRole("checkbox", { name: "Space Age" }).check();
			await page.keyboard.press("Escape");
			await page.getByRole("button", { name: "Search", exact: true }).click();
			await expect(page).toHaveURL(/mods=aai-loaders/);
			await expect(page.getByRole("article")).toHaveCount(3);

			await page.goto(`/browse?q=${word}&mods=space-age`);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(card(page, spaceAge.title)).toBeVisible();
		});

		test("filters by kind, counts each kind, and cards show it", async ({ page, seed }) => {
			const word = uniqueWord();
			const blueprint = await seed.entry({ title: `Print ${word}`, blueprint: BLUEPRINT });
			const book = await seed.entry({
				title: `Tools ${word}`,
				// Each result matches its own text, so the book's blueprint and
				// planner carry the word themselves.
				blueprint: bookOf("Tools", [
					blueprintOf(`Furnaces ${word}`, ["stone-furnace"]),
					relabelPlanner(plannerFrom(DECONSTRUCTION_BOOK, "Cliffs"), `Inner ${word}`),
				]),
			});
			const planner = await seed.entry({
				title: `Cliffs ${word}`,
				blueprint: plannerFrom(DECONSTRUCTION_BOOK, "Cliffs"),
			});
			for (const entry of [blueprint, book, planner]) await seed.searchable(entry, word);
			const exactCard = (title: string) =>
				page
					.getByRole("article")
					.filter({ has: page.getByRole("heading", { name: title, exact: true }) });

			// By default blueprints only: the single blueprint and the book's.
			await page.goto(`/browse?q=${word}`);
			await expect(page.getByRole("article")).toHaveCount(2);
			// Nothing renders here: a blueprint's card shows its preview is on its way.
			await expect(
				card(page, blueprint.title).locator("[data-card-preview]"),
			).toHaveAttribute("aria-busy", "true");

			await page.getByText("Advanced search").click();
			await page.locator("#search-kind-value").click();
			// Each kind shows how many results it has; Blueprint is ticked.
			await expect(page.getByRole("checkbox", { name: /^Blueprint\s+2$/ })).toBeChecked();
			await expect(
				page.getByRole("checkbox", { name: /^Blueprint book\s+1$/ }),
			).toBeVisible();
			await expect(
				page.getByRole("checkbox", { name: /^Deconstruction planner\s+2$/ }),
			).toBeVisible();
			await expect(
				page.getByRole("checkbox", { name: /^Upgrade planner\s+0$/ }),
			).toBeVisible();
			await page.getByRole("checkbox", { name: /^Blueprint\s+2$/ }).uncheck();
			await page.getByRole("checkbox", { name: /^Blueprint book/ }).check();
			await page.getByRole("checkbox", { name: /^Deconstruction planner/ }).check();
			await page.keyboard.press("Escape");
			await page.getByRole("button", { name: "Search", exact: true }).click();
			await expect(page).toHaveURL(/kind=book&kind=deconstruction_planner/);
			// The book, the planner inside it and the single planner.
			await expect(page.getByRole("article")).toHaveCount(3);
			await expect(card(page, blueprint.title)).toHaveCount(0);
			await expect(
				card(page, book.title).getByRole("img", { name: "Blueprint book" }),
			).toBeVisible();
			// A planner never gets a preview.
			await expect(
				exactCard(planner.title).locator("[data-card-preview]"),
			).not.toHaveAttribute("aria-busy");
			await expect(
				exactCard(planner.title).getByRole("img", { name: "Deconstruction planner" }),
			).toBeVisible();
			// The counts still cover every kind.
			await page.locator("#search-kind-value").click();
			await expect(page.getByRole("checkbox", { name: /^Blueprint\s+2$/ })).toBeVisible();
			await page.keyboard.press("Escape");

			// A planner inside the book links its book and opens its row there.
			const inBook = exactCard(`Inner ${word}`);
			await expect(
				inBook.getByRole("link", { name: `In book ${book.title}` }),
			).toHaveAttribute("href", `/e/${book.id}`);
			await inBook.getByRole("heading", { name: `Inner ${word}`, exact: true }).click();
			await expect(page).toHaveURL(new RegExp(`/e/${book.id}#node-`));
			await expect(page.locator("details.planner[open]")).toHaveCount(1);
			await page.goBack();

			// Unmeasured results (no analysis yet, planners) never fit.
			await page.goto(`/browse?q=${word}&footprint_width=1000&footprint_height=1000`);
			await expect(page.getByText(`No results found for "${word}".`)).toBeVisible();
			// The empty state drops the filters but keeps the search text.
			await page.getByRole("button", { name: "Clear filters" }).click();
			await expect(page).toHaveURL(new RegExp(`/browse\\?q=${word}$`));
			await expect(page.getByRole("article")).toHaveCount(2);
		});

		test("lists a book's blueprints as results that match their own text and link their book", async ({
			page,
			seed,
		}) => {
			const word = uniqueWord();
			const beltWord = uniqueWord("belt");
			const book = await seed.entry({
				title: `Smelting ${word}`,
				blueprint: bookOf("Smelting", [
					blueprintOf(`Furnaces ${word}`, ["stone-furnace", "inserter"]),
					blueprintOf(`Belts ${beltWord}`, ["transport-belt"]),
				]),
			});
			await seed.searchable(book, word);

			// The book and the blueprint that carries the word: the book's text
			// does not make its other blueprint match.
			await page.goto(`/browse?q=${word}&kind=blueprint&kind=book`);
			await expect(page.getByRole("article")).toHaveCount(2);
			await expect(card(page, `Belts ${beltWord}`)).toHaveCount(0);
			await page.goto(`/browse?q=${word}&kind=blueprint&sort=relevance`);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(page.getByRole("article").first()).toContainText(`Furnaces ${word}`);

			// A blueprint matches its own text and links the book it is in.
			await page.goto(`/browse?q=${beltWord}&kind=blueprint`);
			const leaf = card(page, `Belts ${beltWord}`);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(leaf.getByRole("img", { name: "Blueprint", exact: true })).toBeVisible();
			await expect(leaf.getByRole("link", { name: `In book ${book.title}` })).toHaveAttribute(
				"href",
				`/e/${book.id}`,
			);

			await leaf.getByRole("link", { name: `Belts ${beltWord}` }).click();
			await expect(page).toHaveURL(new RegExp(`/e/${book.id}/b/[^/?]+$`));
			await page.goBack();
			await card(page, `Belts ${beltWord}`)
				.getByRole("link", { name: `In book ${book.title}` })
				.click();
			await expect(page).toHaveURL(`/e/${book.id}`);
		});

		test("finds a blueprint by its community name", async ({ page, seed }) => {
			const word = uniqueWord();
			const internal = await seed.entry({ title: `Automation science pack ${word}` });
			const community = await seed.entry({ title: `Red science ${word}` });
			const other = await seed.entry({ title: `Gears ${word}` });
			for (const entry of [internal, community, other]) await seed.searchable(entry, word);

			// "red science" finds what the item's own name finds, and the other way round.
			for (const query of [
				`red science ${word}`,
				`automation science pack ${word}`,
				`automation-science-pack ${word}`,
			]) {
				await page.goto(`/browse?q=${encodeURIComponent(query)}`);
				await expect(page.getByRole("article")).toHaveCount(2);
				await expect(card(page, internal.title)).toBeVisible();
				await expect(card(page, community.title)).toBeVisible();
			}
			await page.goto(`/browse?q=${word}`);
			await expect(page.getByRole("article")).toHaveCount(3);
		});

		test("filters by the upload date range", async ({ page, seed, request }) => {
			const word = uniqueWord();
			const march = await seed.entry({ title: `March ${word}` });
			const june = await seed.entry({ title: `June ${word}` });
			const today = await seed.entry({ title: `Today ${word}` });
			for (const entry of [march, june, today]) await seed.searchable(entry, word);
			setEntryCreatedAt(march.id, "2025-03-10T12:00:00Z");
			// Late in its day: a "before" date covers the whole day.
			setEntryCreatedAt(june.id, "2025-06-15T23:30:00Z");
			await expect
				.poll(async () => {
					const response = await request.get(`${apiURL}/api/v1/search`, {
						params: { q: word, created_before: "2025-12-31" },
					});
					return ((await response.json()) as { total: number }).total;
				})
				.toBe(2);

			await page.goto(`/browse?q=${word}`);
			await expect(page.getByRole("article")).toHaveCount(3);
			await page.getByText("Advanced search").click();
			await page.getByLabel("Uploaded on or after").fill("2025-03-01");
			await page.getByLabel("Uploaded on or before").fill("2025-06-15");
			await page.getByRole("button", { name: "Search blueprints" }).click();
			await expect(page).toHaveURL(/created_after=2025-03-01&created_before=2025-06-15/);
			await expect(page.getByRole("article")).toHaveCount(2);
			await expect(card(page, march.title)).toBeVisible();
			await expect(card(page, june.title)).toBeVisible();
			// The inputs keep what was asked, and either bound works alone.
			await expect(page.getByLabel("Uploaded on or after")).toHaveValue("2025-03-01");
			await page.goto(`/browse?q=${word}&created_before=2025-06-14`);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(card(page, march.title)).toBeVisible();
			await page.goto(`/browse?q=${word}&created_after=2025-06-16`);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(card(page, today.title)).toBeVisible();

			// A date that is not one is ignored by the page; the API refuses it.
			await page.goto(`/browse?q=${word}&created_after=2025-02-31`);
			await expect(page.getByRole("article")).toHaveCount(3);
			const refused = await request.get(`${apiURL}/api/v1/search`, {
				params: { created_after: "2025-02-31" },
			});
			expect(refused.status()).toBe(400);
		});

		test("counts the filter options without the filter itself", async ({ page, seed }) => {
			const word = uniqueWord();
			// Space Age content, tagged.
			const spaceAge = await seed.entry({
				title: `Fish ${word}`,
				blueprint: SPACE_AGE_20,
				tags: ["rail-grid"],
			});
			const vanilla = await seed.entry({
				title: `Belts ${word}`,
				blueprint: blueprintOf("Belts", ["transport-belt", "inserter"]),
				tags: ["rail-grid"],
			});
			const newer = await seed.entry({ title: `Newer ${word}`, blueprint: BLUEPRINT_21 });
			const platform = await seed.entry({
				title: `Ship ${word}`,
				blueprint: blueprintOf("Ship", ["transport-belt"]),
			});
			for (const entry of [spaceAge, vanilla, newer, platform])
				await seed.searchable(entry, word);
			storeAnalysis(platform.id, { width: 3, height: 3, platform: true });
			reindexEntry(platform.id);
			await expect
				.poll(async () => {
					const response = await page.request.get(`${apiURL}/api/v1/search`, {
						params: { q: word, blueprint_type: "platforms" },
					});
					return ((await response.json()) as { total: number }).total;
				})
				.toBe(1);

			// Opening and closing a picker is a click on its summary.
			const toggle = (filter: string) =>
				page
					.locator(`#search-${filter}-value`)
					.evaluate((el) => el.closest("summary")?.click());
			await page.goto(`/browse?q=${word}`);
			await page.getByText("Advanced search").click();

			// Tags: the two tagged blueprints.
			// The count sits inside the pill as a badge and reads after the name; a
			// pill without matches stays listed, muted and without a badge.
			await toggle("tags");
			await expect(
				page.getByRole("checkbox", { name: /^Rail grid, 2 results$/ }),
			).toBeVisible();
			await expect(
				page.locator("label", { hasText: "Rail grid" }).locator(".badge"),
			).toHaveText("2");
			await expect(
				page.getByRole("checkbox", { name: /^Steam power, 0 results$/ }),
			).toBeVisible();
			await expect(
				page.locator("label", { hasText: "Steam power" }).locator(".badge"),
			).toBeHidden();
			await toggle("tags");
			// Ground and space platform blueprints.
			await expect(page.locator('[data-count="all"]')).toHaveText("4");
			await expect(page.locator('[data-count="factories"]')).toHaveText("3");
			await expect(page.locator('[data-count="platforms"]')).toHaveText("1");
			// Vanilla and Space Age.
			await toggle("mods");
			await expect(
				page.getByRole("checkbox", { name: /^Vanilla only, 3 results$/ }),
			).toBeVisible();
			await expect(
				page.getByRole("checkbox", { name: /^Space Age, 1 result$/ }),
			).toBeVisible();
			await toggle("mods");
			// Game versions: three made in 2.0, one in 2.1.
			const version = (name: RegExp) => page.getByRole("radio", { name });
			await toggle("game-version");
			await expect(version(/^Before 2\.0 \(0\.x, 1\.x\)\s+0$/)).toBeAttached();
			await expect(version(/^v2\.0\s+3$/)).toBeAttached();
			await expect(version(/^v2\.1\s+1$/)).toBeAttached();
			await toggle("game-version");

			// With the rail-grid tag chosen, the other counts narrow to its
			// results (one Space Age, one vanilla, no platform) while the tags
			// stay counted without their own filter.
			// The panel opens by itself: a filter is set.
			await page.goto(`/browse?q=${word}&tags=rail-grid`);
			await expect(page.locator('[data-count="factories"]')).toHaveText("2");
			await expect(page.locator('[data-count="platforms"]')).toHaveText("0");
			await toggle("mods");
			await expect(
				page.getByRole("checkbox", { name: /^Vanilla only, 1 result$/ }),
			).toBeVisible();
			await expect(
				page.getByRole("checkbox", { name: /^Space Age, 1 result$/ }),
			).toBeVisible();
			await toggle("mods");
			await toggle("tags");
			await expect(
				page.getByRole("checkbox", { name: /^Rail grid, 2 results$/ }),
			).toBeChecked();
			// A ticked pill shows its badge.
			await expect(
				page.locator("label", { hasText: "Rail grid" }).locator(".badge"),
			).toBeVisible();
			await toggle("tags");
			await toggle("game-version");
			await expect(version(/^v2\.0\s+2$/)).toBeAttached();
			await expect(version(/^v2\.1\s+0$/)).toBeAttached();
		});

		test("picks a game version from a single-choice dropdown", async ({ page, seed }) => {
			const word = uniqueWord();
			const older = await seed.entry({ title: `Older ${word}`, blueprint: SPACE_AGE_20 });
			const newer = await seed.entry({ title: `Newer ${word}`, blueprint: BLUEPRINT_21 });
			await seed.searchable(older, word);
			await seed.searchable(newer, word);

			await page.goto(`/browse?q=${word}`);
			await page.getByText("Advanced search").click();
			const field = page.locator("#search-game-version-value");
			const summary = page.locator("summary", { has: field });
			const dropdown = page.locator("details.dropdown", { has: field });
			const option = (name: RegExp) => page.getByRole("radio", { name });
			await expect(field).toHaveText("Any version");

			// A click chooses: the dropdown closes, shows the choice, and the search uses it.
			await summary.click();
			await expect(dropdown).toHaveJSProperty("open", true);
			await expect(option(/^Any version$/)).toBeChecked();
			await option(/^v2\.1\s+1$/).click();
			await expect(dropdown).toHaveJSProperty("open", false);
			await expect(field).toHaveText("v2.1");
			await page.locator(".advanced-search-submit").click();
			await expect(page).toHaveURL(/game_version=v2\.1(&|$)/);
			await expect(page.getByRole("article")).toHaveCount(1);
			await expect(card(page, newer.title)).toBeVisible();
			// The chosen option is the checked one after the reload.
			await expect(field).toHaveText("v2.1");
			await summary.click();
			await expect(dropdown).toHaveJSProperty("open", true);
			await expect(option(/^v2\.1\s+1$/)).toBeChecked();

			// Escape closes without changing the choice and returns focus to the field.
			await page.keyboard.press("Escape");
			await expect(dropdown).toHaveJSProperty("open", false);
			await expect(summary).toBeFocused();
			await expect(field).toHaveText("v2.1");

			// A click outside closes it too.
			await summary.click();
			await expect(dropdown).toHaveJSProperty("open", true);
			await page.locator("body").click({ position: { x: 2, y: 300 } });
			await expect(dropdown).toHaveJSProperty("open", false);

			// The keyboard: Enter opens, the arrows move the choice, Enter chooses.
			await summary.focus();
			await page.keyboard.press("Enter");
			await expect(dropdown).toHaveJSProperty("open", true);
			await page.keyboard.press("Tab");
			await expect(option(/^v2\.1\s+1$/)).toBeFocused();
			await page.keyboard.press("ArrowUp");
			await expect(option(/^v2\.0\s+/)).toBeFocused();
			await expect(dropdown).toHaveJSProperty("open", true);
			await page.keyboard.press("Enter");
			await expect(dropdown).toHaveJSProperty("open", false);
			await expect(summary).toBeFocused();
			await expect(field).toHaveText("v2.0");
			await page.locator(".advanced-search-submit").click();
			await expect(page).toHaveURL(/game_version=v2\.0(&|$)/);
			await expect(card(page, older.title)).toBeVisible();
			await expect(page.getByRole("article")).toHaveCount(1);

			// "Any version" undoes it.
			await summary.click();
			await option(/^Any version$/).click();
			await expect(field).toHaveText("Any version");
			await page.locator(".advanced-search-submit").click();
			await expect(page.getByRole("article")).toHaveCount(2);
		});

		test("says when nothing matches", async ({ page }) => {
			const word = uniqueWord("nothing");
			await page.goto("/browse");
			await search(page, word);
			await expect(page.getByText(`No results found for "${word}".`)).toBeVisible();
			await expect(page.getByRole("button", { name: "Upload a blueprint" })).toHaveAttribute(
				"href",
				"/upload",
			);
			await page.getByRole("button", { name: "Clear search text" }).click();
			await expect(page).toHaveURL(/\/browse$/);
			await expect(page.getByRole("button", { name: "Clear filters" })).toHaveCount(0);
		});
	});
}

test.describe("as the uploader", () => {
	test.use({ viewer: "member" });

	test("cards name the uploader and link to their profile", async ({ page, seed, me }) => {
		const entry = await seed.entry({ title: `Owned ${uniqueWord()}`, owner: me });
		await seed.searchable(entry);

		await page.goto(`/browse?q=${encodeURIComponent(entry.title)}`);
		await card(page, entry.title).getByRole("link", { name: me.username }).click();
		await expect(page).toHaveURL(`/u/${me.username}`);
		await expect(page.getByRole("heading", { level: 1, name: me.username })).toBeVisible();
	});
});
