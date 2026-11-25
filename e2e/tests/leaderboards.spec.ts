// The leaderboard (/leaderboard): a Ships / Factories switch, each shown board
// with its top three as cards and places 4 to 10 behind a disclosure, boards
// still short of ten entries in one compact list. Ten entries open a board;
// the leader reaches the home page's records and, once the minute task
// records it, its owner's profile, a trophy chip and a primary ring on its
// cards, and a record row on its entry page. The e2e stack runs no Factorio,
// so the analyses are stored straight in the database (`storeAnalysis`); the
// boards are global, so this spec's entries beat any other test's (and an
// earlier run's). The two tests share the boards, so they run in order.
import type { Locator, Page } from "@playwright/test";
import { uniqueWord, type SeededEntry } from "../support/api.ts";
import { PLATFORM, relabel } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { storeAnalysis } from "../support/stack.ts";

test.describe.configure({ mode: "serial" });

const SHIP_BOARDS = ["Fastest ship", "Highest thrust-to-weight", "Lightest ship", "Most cargo"];
const FACTORY_BOARDS = [
	"Most science",
	"Densest science",
	"Cleanest science",
	"Biggest power plant",
];

const board = (page: Page, name: string) => page.getByRole("region", { name });

/** Newer runs score higher, so an earlier run's entries on a reused stack never lead. */
const runBase = () => 1000 + (Math.floor(Date.now() / 1000) % 1_000_000);

/** Polls a page until the locator's text contains `text` (the 30 s board cache, the minute task). */
async function pollFor(page: Page, url: string, locator: Locator, text: string, timeout: number) {
	await expect
		.poll(
			async () => {
				await page.goto(url);
				return (await locator.count()) > 0 ? locator.first().textContent() : "";
			},
			{ timeout, intervals: [2_000, 5_000] },
		)
		.toContain(text);
}

test("switches between ships and factories and lists the boards still filling up", async ({
	page,
}) => {
	await page.goto("/");
	await page
		.getByRole("navigation", { name: "Sidebar" })
		.getByRole("link", { name: "Leaderboard" })
		.click();
	await expect(page).toHaveURL("/leaderboard");
	await expect(page.getByRole("heading", { level: 1, name: "Leaderboard" })).toBeVisible();
	const groups = page.getByRole("navigation", { name: "Leaderboard group" });
	await expect(groups.getByRole("link", { name: "Ships" })).toHaveAttribute(
		"aria-current",
		"page",
	);
	const hidden = (name: string) => page.locator("[data-board-hidden]").filter({ hasText: name });
	// No test makes ten ships with cargo bays, nor ten power plants.
	await expect(hidden("Most cargo")).toContainText(/of 10 entries so far/);
	for (const name of SHIP_BOARDS) {
		await expect(
			board(page, name).or(page.locator("[data-board-hidden]").filter({ hasText: name })),
		).toBeVisible();
	}

	await groups.getByRole("link", { name: "Factories" }).click();
	await expect(page).toHaveURL("/leaderboard?group=factories");
	await expect(groups.getByRole("link", { name: "Factories" })).toHaveAttribute(
		"aria-current",
		"page",
	);
	await expect(hidden("Biggest power plant")).toContainText(
		"The most electricity left over once the build has powered itself.",
	);
	for (const name of FACTORY_BOARDS) {
		await expect(
			board(page, name).or(page.locator("[data-board-hidden]").filter({ hasText: name })),
		).toBeVisible();
	}
	await expect(page.getByText("Fastest ship")).toHaveCount(0);
});

test("ten ships open the fastest board; the leader reaches the home page and its owner's profile", async ({
	page,
	seed,
}) => {
	// The boards are cached for 30 s and the record is stored by a minute task.
	test.setTimeout(180_000);
	const owner = await seed.account();
	const word = uniqueWord();
	const base = runBase();
	const ships: SeededEntry[] = [];
	for (let i = 0; i < 10; i++) {
		const title = `Ship ${word} ${i}`;
		const entry = await seed.entry({ title, owner, blueprint: relabel(PLATFORM, title) });
		storeAnalysis(entry.id, {
			width: 20,
			height: 20,
			platform: { speed: base + i, thrust: 50, weight: 500 },
		});
		ships.push(entry);
	}
	const fastest = ships[9];
	const leaderValue = `${(base + 9).toLocaleString("en-US")} km/s`;

	const fastestBoard = board(page, "Fastest ship");
	await pollFor(page, "/leaderboard", fastestBoard, fastest.title, 60_000);
	await expect(
		fastestBoard.getByText("The highest top speed of a space platform that moves."),
	).toBeVisible();
	const leader = fastestBoard.locator("[data-noted-card]").first();
	await expect(leader).toContainText("#1");
	await expect(leader).toContainText(leaderValue);
	await expect(fastestBoard.locator("[data-noted-card]").nth(1)).toContainText(ships[8].title);
	// Places 4 to 10 wait behind the disclosure.
	const more = fastestBoard.locator("details");
	await expect(more.getByText(ships[6].title)).toBeHidden();
	await more.getByText("Places 4 to 10").click();
	await expect(more.getByRole("link", { name: ships[6].title })).toBeVisible();
	await leader.getByRole("link", { name: fastest.title }).first().click();
	await expect(page).toHaveURL(new RegExp(`/e/${fastest.id}`));

	await page.goto("/");
	const records = page.getByRole("region", { name: "Leaderboard" });
	const recordCard = records.locator("[data-noted-card]").filter({ hasText: "Fastest ship" });
	await expect(recordCard).toContainText(fastest.title);
	// The record's note sits in the book row's place, without the book icon.
	const recordCaption = recordCard.locator("[data-card-caption]");
	await expect(recordCaption).toContainText("Fastest ship");
	await expect(recordCaption.locator("svg")).toHaveCount(0);
	await expect(records.getByRole("link", { name: "All records" })).toHaveAttribute(
		"href",
		"/leaderboard",
	);

	// The minute task records the lead and awards it: a permanent badge and +50 XP.
	const progress = page.locator("[data-profile-progress]");
	await pollFor(page, `/u/${owner.username}`, progress, "Record holder", 90_000);
	// A run of the task while the ships were still being added recorded an
	// earlier leader: the next run moves the record to the fastest.
	const held = progress.getByRole("list", { name: "Records held" });
	await pollFor(page, `/u/${owner.username}`, held, fastest.title, 90_000);
	await expect(held).toContainText(`Fastest ship: ${fastest.title}`);
	await expect(progress.getByRole("link", { name: "Fastest ship" })).toHaveAttribute(
		"href",
		"/leaderboard#board-fastest",
	);
});

test("ten science builds open the factory boards; the leader's cards and page show the record", async ({
	page,
	seed,
}) => {
	test.setTimeout(180_000);
	const owner = await seed.account();
	const word = uniqueWord();
	// Science packs a second: 60 SPM and up, so every build ranks on all three
	// science boards; the same 20x20 footprint, so the most science is also
	// the densest and the cleanest.
	const base = runBase() / 1000;
	const builds: SeededEntry[] = [];
	for (let i = 0; i < 10; i++) {
		const entry = await seed.entry({ title: `Science ${word} ${i}`, owner });
		storeAnalysis(entry.id, { width: 20, height: 20, science: base + i });
		builds.push(entry);
	}
	const top = builds[9];

	const factories = "/leaderboard?group=factories";
	const mostScience = board(page, "Most science");
	await pollFor(page, factories, mostScience, top.title, 60_000);
	await expect(mostScience).toContainText("every pack and quality summed");
	await expect(mostScience.locator("[data-noted-card]").first()).toContainText(
		`${((base + 9) * 60).toLocaleString("en-US", { maximumFractionDigits: 1 })} SPM`,
	);
	await expect(board(page, "Densest science").locator("[data-noted-card]").first()).toContainText(
		top.title,
	);

	// Once the minute task stores the leader, its cards carry the record: a
	// trophy chip naming the board beside the version badge, and a ring in the
	// theme's primary colour that takes no space.
	const leaderCard = mostScience.locator("[data-noted-card]").first().locator(".entry-card");
	await pollFor(
		page,
		factories,
		leaderCard.locator("[data-card-record]"),
		"#1 Most science",
		90_000,
	);
	const chip = leaderCard.locator("[data-card-record]");
	await expect(chip).toContainText("+2");
	await expect(chip).toHaveAttribute("title", /Holds the Most science record/);
	await expect(leaderCard).toHaveAttribute("data-record", "");
	const ring = await leaderCard.evaluate((card) => {
		const primary = getComputedStyle(card).getPropertyValue("--pico-primary").trim();
		const probe = document.createElement("span");
		probe.style.color = primary;
		document.body.append(probe);
		const color = getComputedStyle(probe).color;
		probe.remove();
		return { shadow: getComputedStyle(card).boxShadow, color };
	});
	expect(ring.shadow).toContain(`${ring.color} 0px 0px 0px 2px`);
	// A card that holds nothing has neither.
	const runnerUp = mostScience.locator("[data-noted-card]").nth(1).locator(".entry-card");
	await expect(runnerUp.locator("[data-card-record]")).toHaveCount(0);
	await expect(runnerUp).not.toHaveAttribute("data-record", "");

	// The same chip on the home page's records, a search result and the entry page.
	await page.goto("/");
	await expect(
		page
			.getByRole("region", { name: "Leaderboard" })
			.locator("[data-noted-card]")
			.filter({ hasText: "Most science" })
			.locator("[data-card-record]"),
	).toContainText("#1 Most science");
	await page.goto(`/u/${owner.username}`);
	await expect(
		page.locator(".entry-card").filter({ hasText: top.title }).locator("[data-card-record]"),
	).toContainText("#1 Most science");
	await page.goto(`/e/${top.id}`);
	const row = page.locator("[data-entry-records]");
	await expect(row.getByRole("link", { name: "#1 Most science" })).toHaveAttribute(
		"href",
		"/leaderboard?group=factories#board-most_science",
	);
	await expect(row.getByRole("link")).toHaveCount(3);
	await page.goto(`/e/${builds[0].id}`);
	await expect(page.locator("[data-entry-records]")).toHaveCount(0);
});
