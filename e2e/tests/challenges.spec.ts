// Design challenges, the app's focal point: the list by status and a
// challenge's page through its timeline (upcoming, open, voting, closed),
// entering (picked or uploaded, replaced, withdrawn, lost by leaving public),
// votes and their options, the rule check (environment, restrictions, new
// revisions only, analysis states, staff overrides), ranking, and the
// podiums of an ended challenge. The e2e stack runs no Factorio, so analyses
// are stored straight in the database (`storeAnalysis`) and the console's
// staff work is done there too (`createChallenge`, `setChallengePhase`).
import type { Locator, Page } from "@playwright/test";
import { type Account, type SeededEntry, type Seeder, uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BLUEPRINT_21, BOOK, relabel, SPACE_AGE_20 } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import {
	ageAccount,
	ageRevisions,
	type ChallengeSettings,
	createChallenge,
	failAnalysis,
	overrideChallengeEntry,
	setChallengePhase,
	storeAnalysis,
} from "../support/stack.ts";
import { ready } from "../support/ui.ts";

type Challenge = { slug: string; title: string };

function newChallenge(settings: ChallengeSettings = {}): Challenge {
	const word = uniqueWord();
	const slug = word.toLowerCase().replaceAll(/[^a-z0-9]+/g, "-");
	const title = `Tiny ${word.replaceAll(/[^A-Za-z0-9]+/g, " ")}`;
	createChallenge(slug, title, settings);
	return { slug, title };
}

/**
 * Creates the owner's public entry of `blueprint` and enters it in the
 * challenge. Only entries that meet the rules are public, so it gets a stored
 * analysis that does (a 4x4 footprint) unless `analysed` is false: then it
 * waits for one, as a fresh upload does.
 */
async function entered(
	seed: Seeder,
	owner: Account,
	challenge: Challenge,
	options: { title?: string; blueprint?: string; analysed?: boolean } = {},
): Promise<SeededEntry> {
	const title = options.title ?? `Entry ${uniqueWord()}`;
	// A string of its own: the API reuses one identical string's analysis for another.
	const entry = await seed.entry({
		title,
		owner,
		blueprint: relabel(options.blueprint ?? BLUEPRINT_21, title),
	});
	await seed.challengeEntry(owner, challenge.slug, entry);
	if (options.analysed !== false) storeAnalysis(entry.id, { width: 4, height: 4 });
	return entry;
}

const open = (page: Page, challenge: Challenge) => page.goto(`/challenges/${challenge.slug}`);
const entries = (page: Page) => page.getByRole("region", { name: /^(Entries|Results)/ });
/** One entry's card on the challenge page, with its rank, score or reasons and votes. */
const entryOf = (page: Page, entry: SeededEntry) =>
	entries(page)
		.locator(".entry")
		.filter({ has: page.getByRole("link", { name: entry.title }) });
/** `count` fresh accounts, one after another (each is a passkey sign-up). */
async function accounts(seed: Seeder, count: number): Promise<Account[]> {
	const made: Account[] = [];
	for (let i = 0; i < count; i++) made.push(await seed.account());
	return made;
}

const entriesHeading = (page: Page) =>
	page.getByRole("heading", { level: 2, name: /^(Entries|Results)/ });
const statusLine = (page: Page) => page.locator(".challenge-header > p").first();
const dates = (page: Page) => page.locator(".challenge-header dl");

/** A podium's place by name ("1st place"). */
const place = (podium: Locator, name: string) => podium.getByRole("listitem", { name });

/** Anyone else sees only entries that meet the rules: not this one. */
async function expectNotPublic(page: Page, entry: SeededEntry): Promise<void> {
	await expect(entryOf(page, entry)).toHaveCount(0);
}

/** The signed-in viewer's own entry: its card, score or why it is not ranked, change and withdraw. */
const yourEntry = (page: Page) => page.getByRole("region", { name: "Your entry" });
/** The entry picker's rows (the viewer's public single blueprints). */
const pickerRows = (page: Page) => yourEntry(page).locator("[data-picker-list] button");

/**
 * A rejected entry is public to no one but its entrant, who reads the reasons,
 * expanded, in their own entry section (guests: `expectNotPublic`).
 */
async function expectRejected(page: Page, reason: string | RegExp): Promise<void> {
	await expect(yourEntry(page).getByText("Not ranked")).toBeVisible();
	await expect(yourEntry(page).locator("details li")).toContainText([reason]);
}

test.describe("as a guest", () => {
	test("sees the challenge listed and its rules, and is asked to log in", async ({ page }) => {
		const challenge = newChallenge({ description: "Build **small**." });
		await page.goto("/challenges");
		await page.getByRole("link", { name: challenge.title }).click();
		await expect(page.getByRole("heading", { level: 1, name: challenge.title })).toBeVisible();
		await expect(statusLine(page)).toHaveText("Challenge · Open");
		await expect(page.getByText("Lowest footprint area")).toBeVisible();
		await expect(page.getByText("A single blueprint, not a book")).toBeVisible();
		await expect(page.getByText("Base game and Space Age only: no other mods")).toBeVisible();
		await expect(page.getByText(/Uploaded after the challenge started/)).toBeVisible();
		// The description is Markdown.
		await expect(page.locator("section.description strong")).toHaveText("small");
		await expect(page.getByText("Log in to enter this challenge.")).toBeVisible();
		await expect(page.getByRole("link", { name: "Log in" }).last()).toBeVisible();
		await expect(page.getByText("No entries yet.")).toBeVisible();
	});

	test("lists challenges by status; drafts and hidden ones stay unlisted until they start", async ({
		page,
	}) => {
		const upcoming = newChallenge({ phase: "upcoming" });
		const running = newChallenge({ phase: "open" });
		const voting = newChallenge({ phase: "judging" });
		const closed = newChallenge({ phase: "closed" });
		const draft = newChallenge({ draft: true });
		const hidden = newChallenge({ phase: "upcoming", hiddenUntilStart: true });

		await page.goto("/challenges");
		const section = (name: string) =>
			page.locator("section").filter({ has: page.getByRole("heading", { level: 2, name }) });
		await expect(section("Upcoming").getByRole("link", { name: upcoming.title })).toBeVisible();
		await expect(section("Open").getByRole("link", { name: running.title })).toBeVisible();
		await expect(section("Voting").getByRole("link", { name: voting.title })).toBeVisible();
		await expect(section("Past").getByRole("link", { name: closed.title })).toBeVisible();
		await expect(page.getByRole("link", { name: draft.title })).toHaveCount(0);
		await expect(page.getByRole("link", { name: hidden.title })).toHaveCount(0);
		expect((await open(page, draft))?.status()).toBe(404);
		expect((await open(page, hidden))?.status()).toBe(404);

		// A hidden challenge shows once it starts.
		setChallengePhase(hidden.slug, "open");
		expect((await open(page, hidden))?.status()).toBe(200);
		await page.goto("/challenges");
		await expect(section("Open").getByRole("link", { name: hidden.title })).toBeVisible();
	});
});

test.describe("as a member", () => {
	test.use({ viewer: "member" });

	test("follows a challenge through upcoming, open, voting and closed", async ({
		page,
		seed,
		me,
	}) => {
		const challenge = newChallenge({ phase: "upcoming" });
		const mine = await seed.entry({
			title: `Mine ${uniqueWord()}`,
			owner: me,
			blueprint: relabel(BLUEPRINT_21, uniqueWord()),
		});

		// Upcoming: the start and deadline, nothing to enter or vote on.
		await open(page, challenge);
		await expect(statusLine(page)).toHaveText("Challenge · Upcoming");
		await expect(dates(page)).toContainText("Starts");
		await expect(dates(page)).toContainText("Deadline");
		await expect(page.getByRole("heading", { name: "Your entry" })).toBeHidden();
		await expect(page.locator("[data-vote-summary]")).toBeHidden();
		await page.goto(`/challenges/${challenge.slug}/enter`);
		await expect(page.getByText("This challenge is not open for entries.")).toBeVisible();

		// Open: enter one's own entry; voting is open too.
		setChallengePhase(challenge.slug, "open");
		const other = await seed.account();
		const theirs = await entered(seed, other, challenge, { title: `Theirs ${uniqueWord()}` });
		await open(page, challenge);
		await expect(statusLine(page)).toHaveText("Challenge · Open");
		await expect(dates(page)).toContainText("Started");
		await expect(dates(page)).toContainText("Voting ends");
		await ready(page);
		await Promise.all([
			page.waitForEvent("load"),
			pickerRows(page).filter({ hasText: mine.title }).click(),
		]);
		await expect(yourEntry(page).getByRole("heading", { name: mine.title })).toBeVisible();
		await expect(page.getByText("You have 3 of 3 votes left.")).toBeVisible();
		// The heading counts public entries: its own waits for the analysis.
		await expect(entriesHeading(page)).toHaveText("Entries (1)");
		storeAnalysis(mine.id, { width: 3, height: 3 });
		storeAnalysis(theirs.id, { width: 4, height: 4 });

		// Voting: the deadline passed; no more entries, votes still count.
		setChallengePhase(challenge.slug, "judging");
		await open(page, challenge);
		await ready(page);
		await expect(statusLine(page)).toHaveText("Challenge · Voting");
		await expect(dates(page)).toContainText("Voting ends");
		await expect(page.getByRole("heading", { name: "Your entry" })).toBeHidden();
		await Promise.all([
			page.waitForEvent("load"),
			entryOf(page, theirs).getByRole("button", { name: "Vote" }).click(),
		]);
		await expect(page.getByText("You have 2 of 3 votes left.")).toBeVisible();
		await page.goto(`/challenges/${challenge.slug}/enter`);
		await expect(page.getByText("This challenge is not open for entries.")).toBeVisible();

		// Closed: results with the votes, nothing left to do.
		setChallengePhase(challenge.slug, "closed");
		await open(page, challenge);
		await expect(statusLine(page)).toHaveText("Challenge · Closed");
		await expect(dates(page)).toContainText("Ended");
		await expect(entriesHeading(page)).toHaveText("Results (2)");
		await expect(entries(page).getByRole("button", { name: /^Vot/ })).toHaveCount(0);
		await expect(page.locator("[data-vote-summary]")).toBeHidden();
		const winners = page.getByRole("region", { name: "Winners" });
		await expect(winners.getByRole("listitem", { name: "1st place" })).toContainText(
			mine.title,
		);
		await expect(winners.getByRole("listitem", { name: "2nd place" })).toContainText(
			theirs.title,
		);
		const votes = page.getByRole("region", { name: "Community vote" });
		await expect(votes.getByRole("listitem", { name: "1st place" })).toContainText(
			theirs.title,
		);
		await expect(votes.getByRole("listitem", { name: "1st place" })).toContainText("1 vote");
	});

	test("enters a public blueprint, sees it on the entry, loses it by making it unlisted", async ({
		page,
		seed,
		me,
	}) => {
		const challenge = newChallenge();
		const word = uniqueWord();
		const entry = await seed.entry({
			title: `Mine ${word}`,
			owner: me,
			blueprint: relabel(BLUEPRINT_21, uniqueWord()),
		});
		// Books and private entries are not offered.
		await seed.entry({ title: `Book ${word}`, owner: me, blueprint: BOOK });
		await seed.entry({ title: `Hidden ${word}`, owner: me, visibility: "private" });

		await open(page, challenge);
		await ready(page);
		await expect(pickerRows(page)).toHaveText([entry.title]);
		// The search narrows the list by title.
		await yourEntry(page).getByRole("searchbox").fill("no such blueprint");
		await expect(yourEntry(page)).toContainText("No public blueprint of yours matches.");
		await yourEntry(page).getByRole("searchbox").fill(word);
		await expect(pickerRows(page)).toHaveText([entry.title]);
		await Promise.all([page.waitForEvent("load"), pickerRows(page).first().click()]);
		await expect(yourEntry(page).getByRole("heading", { name: entry.title })).toBeVisible();
		await expect(yourEntry(page)).toContainText("Waiting for the analysis.");
		await expect(entries(page)).toContainText("Your entry");
		await expect(entries(page)).toContainText("Waiting for the analysis.");

		await page.goto(`/e/${entry.id}`);
		await page.getByRole("link", { name: challenge.title }).click();
		await expect(page).toHaveURL(new RegExp(`/challenges/${challenge.slug}$`));

		const response = await me.api.patch(`/api/v1/entries/${entry.id}`, {
			data: { visibility: "unlisted" },
		});
		expect(response.ok()).toBe(true);
		await page.reload();
		await expect(page.getByText("No entries yet.")).toBeVisible();
		await expect(yourEntry(page).getByRole("heading", { level: 3 })).toBeHidden();
	});

	test("replaces its entry with another and withdraws it", async ({ page, seed, me }) => {
		const challenge = newChallenge();
		const first = await entered(seed, me, challenge, { title: `First ${uniqueWord()}` });
		const second = await seed.entry({
			title: `Second ${uniqueWord()}`,
			owner: me,
			blueprint: relabel(BLUEPRINT_21, uniqueWord()),
		});

		await open(page, challenge);
		await expect(yourEntry(page).getByRole("heading", { name: first.title })).toBeVisible();
		// The picker opens on request; the entered one is listed but cannot be picked again.
		await ready(page);
		await expect(yourEntry(page).locator("[data-challenge-picker]")).toBeHidden();
		await yourEntry(page).getByRole("button", { name: "Change entry" }).click();
		await expect(pickerRows(page)).toHaveCount(2);
		await expect(pickerRows(page).filter({ hasText: first.title })).toBeDisabled();
		page.once("dialog", (dialog) => void dialog.accept());
		await Promise.all([
			page.waitForEvent("load"),
			pickerRows(page).filter({ hasText: second.title }).click(),
		]);
		await expect(yourEntry(page).getByRole("heading", { name: second.title })).toBeVisible();
		await expect(entryOf(page, first)).toHaveCount(0);
		await expect(entriesHeading(page)).toHaveText("Entries (0)");

		await ready(page);
		page.once("dialog", (dialog) => void dialog.accept());
		await Promise.all([
			page.waitForEvent("load"),
			page.getByRole("button", { name: "Withdraw" }).click(),
		]);
		await expect(page.getByText("No entries yet.")).toBeVisible();
		await expect(yourEntry(page).getByRole("heading", { level: 3 })).toBeHidden();
	});

	test("uploads a blueprint of any game version on the challenge's upload page, never a book", async ({
		page,
	}) => {
		const challenge = newChallenge();
		const label = `Upload ${uniqueWord()}`;
		await open(page, challenge);
		await page.getByRole("link", { name: "upload a new blueprint for this challenge" }).click();
		await expect(
			page.getByRole("heading", { level: 1, name: `Enter ${challenge.title}` }),
		).toBeVisible();

		// A book is refused before anything is uploaded.
		const field = page.getByLabel("Blueprint exchange string");
		await field.fill(BOOK);
		await page.getByRole("button", { name: "Upload and enter" }).click();
		await expect(page.getByRole("status")).toContainText(
			"Enter a single blueprint, not a book or a planner.",
		);

		// A Factorio 2.0 blueprint is fine: only the rules decide.
		await field.fill(relabel(BLUEPRINT, label));
		await page.getByRole("button", { name: "Upload and enter" }).click();
		await expect(page).toHaveURL(new RegExp(`/challenges/${challenge.slug}$`));
		// Titled after the blueprint's label.
		await expect(yourEntry(page).getByRole("heading", { name: label })).toBeVisible();
		await expect(entries(page)).toContainText("Waiting for the analysis.");
	});

	test("votes for another entry and takes the vote back, never on its own", async ({
		page,
		seed,
		me,
	}) => {
		const challenge = newChallenge();
		const other = await seed.account();
		await entered(seed, other, challenge, { title: `Theirs ${uniqueWord()}` });
		await entered(seed, me, challenge, { title: `Mine ${uniqueWord()}` });

		await open(page, challenge);
		await ready(page);
		await expect(page.getByText("You have 3 of 3 votes left.")).toBeVisible();
		const votes = entries(page).getByRole("button", { name: "Vote" });
		// Only the other entry can be voted for.
		await expect(votes).toHaveCount(1);
		await Promise.all([page.waitForEvent("load"), votes.click()]);
		await expect(page.getByText("You have 2 of 3 votes left.")).toBeVisible();
		await expect(entries(page).getByRole("button", { name: "Voted" })).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		await ready(page);
		await Promise.all([
			page.waitForEvent("load"),
			entries(page).getByRole("button", { name: "Voted" }).click(),
		]);
		await expect(page.getByText("You have 3 of 3 votes left.")).toBeVisible();
	});

	test("with one vote each, voting again needs the vote taken back first", async ({
		page,
		seed,
	}) => {
		const challenge = newChallenge({ votesPerUser: 1 });
		const first = await entered(seed, await seed.account(), challenge);
		const second = await entered(seed, await seed.account(), challenge);

		await open(page, challenge);
		await ready(page);
		await expect(page.getByText("You have 1 of 1 votes left.")).toBeVisible();
		await expect(
			page.getByText(/Every account gets 1 vote, never on its own entry/),
		).toBeVisible();
		await Promise.all([
			page.waitForEvent("load"),
			entryOf(page, first).getByRole("button", { name: "Vote" }).click(),
		]);
		await expect(
			page.getByText(
				"You have used all your votes; take one back to vote for another entry.",
			),
		).toBeVisible();
		await expect(entryOf(page, second).getByRole("button", { name: "Vote" })).toBeDisabled();

		await ready(page);
		await Promise.all([
			page.waitForEvent("load"),
			entryOf(page, first).getByRole("button", { name: "Voted" }).click(),
		]);
		await expect(entryOf(page, second).getByRole("button", { name: "Vote" })).toBeEnabled();
	});

	test("only accounts made before the start vote when the challenge says so", async ({
		page,
		seed,
		me,
	}) => {
		const challenge = newChallenge({ votersBeforeStart: true });
		await entered(seed, await seed.account(), challenge);

		await open(page, challenge);
		await expect(
			page.getByText("Only accounts made before the challenge started can vote."),
		).toBeVisible();
		await expect(page.getByText(/only accounts made before the start can vote/)).toBeVisible();
		await expect(entries(page).getByRole("button", { name: "Vote" })).toHaveCount(0);

		ageAccount(me.username);
		await page.reload();
		await expect(page.getByText("You have 3 of 3 votes left.")).toBeVisible();
		await expect(entries(page).getByRole("button", { name: "Vote" })).toHaveCount(1);
	});

	test("without community voting there are no votes, nor a vote podium", async ({
		page,
		seed,
	}) => {
		const challenge = newChallenge({ votingEnabled: false, requireNewRevisions: false });
		const entry = await entered(seed, await seed.account(), challenge);
		storeAnalysis(entry.id, { width: 5, height: 5 });
		// The challenge ends two seconds ago when it closes: an entry made just
		// now would count as a late revision.
		ageRevisions(entry.id);

		await open(page, challenge);
		await expect(page.getByText(/Every account gets/)).toBeHidden();
		await expect(page.locator("[data-vote-summary]")).toBeHidden();
		await expect(entries(page).getByRole("button", { name: "Vote" })).toHaveCount(0);

		setChallengePhase(challenge.slug, "closed");
		await page.reload();
		await expect(page.getByRole("region", { name: "Winners" })).toBeVisible();
		await expect(page.getByRole("region", { name: "Community vote" })).toHaveCount(0);
		await expect(page.getByText(/\d+ votes?$/)).toHaveCount(0);
	});
});

test.describe("scores", () => {
	test("rank accepted entries by the goal; pending, failed and partial analyses are not ranked", async ({
		page,
		seed,
	}) => {
		const challenge = newChallenge();
		const made: SeededEntry[] = [];
		for (const [index, owner] of (await accounts(seed, 6)).entries())
			made.push(
				await entered(seed, owner, challenge, {
					title: `Entry ${index} ${uniqueWord()}`,
					analysed: false,
				}),
			);
		const [small, medium, large, pending, failed, partial] = made;
		storeAnalysis(small.id, { width: 10, height: 5 });
		storeAnalysis(medium.id, { width: 12, height: 7 });
		storeAnalysis(large.id, { width: 20, height: 20 });
		failAnalysis(failed.id);
		storeAnalysis(partial.id, { width: 1, height: 1, partial: true });

		// A live leaderboard shows the scores while open, the lowest footprint first.
		await open(page, challenge);
		await expect(entryOf(page, small).getByLabel("Rank 1")).toBeVisible();
		await expect(entryOf(page, small)).toContainText("Score: 50 tiles");
		await expect(entryOf(page, medium).getByLabel("Rank 2")).toBeVisible();
		await expect(entryOf(page, medium)).toContainText("Score: 84 tiles");
		await expect(entryOf(page, large).getByLabel("Rank 3")).toBeVisible();
		await expect(entryOf(page, large)).toContainText("Score: 400 tiles");
		const titles = entries(page).locator(".entry");
		await expect(titles.nth(0)).toContainText(small.title);
		await expect(titles.nth(1)).toContainText(medium.title);
		await expect(titles.nth(2)).toContainText(large.title);

		// Only scored entries are public: the others are listed for no one else.
		await expect(entriesHeading(page)).toHaveText("Entries (3)");
		for (const hidden of [pending, failed, partial]) await expectNotPublic(page, hidden);
	});

	test("stay hidden until the deadline without a live leaderboard", async ({ page, seed }) => {
		const challenge = newChallenge({ liveLeaderboard: false });
		const entry = await entered(seed, await seed.account(), challenge);
		storeAnalysis(entry.id, { width: 3, height: 4 });

		await open(page, challenge);
		await expect(page.getByText("Scores stay hidden until the deadline.")).toBeVisible();
		await expect(entryOf(page, entry)).toContainText("Meets the rules.");
		await expect(entryOf(page, entry).getByLabel("Rank 1")).toHaveCount(0);

		setChallengePhase(challenge.slug, "judging");
		await page.reload();
		await expect(page.getByText("Scores stay hidden until the deadline.")).toBeHidden();
		await expect(entryOf(page, entry)).toContainText("Score: 12 tiles");
	});

	test("a staff override rejects an entry with its reason, or accepts one the rules refuse", async ({
		page,
		seed,
	}) => {
		const challenge = newChallenge({
			rules: { restrictions: [{ param: "footprint_area", op: "<=", value: 20 }] },
		});
		const disqualified = await entered(seed, await seed.account(), challenge);
		const pardoned = await entered(seed, await seed.account(), challenge);
		storeAnalysis(disqualified.id, { width: 2, height: 2 });
		storeAnalysis(pardoned.id, { width: 10, height: 10 });

		await open(page, challenge);
		await expectNotPublic(page, pardoned);

		overrideChallengeEntry(challenge.slug, disqualified.id, "rejected", "Copied from a guide");
		overrideChallengeEntry(challenge.slug, pardoned.id, "accepted", "");
		await page.reload();
		await expectNotPublic(page, disqualified);
		await expect(entryOf(page, pardoned)).toContainText("Score: 100 tiles");
		await expect(entryOf(page, pardoned).getByLabel("Rank 1")).toBeVisible();
	});
});

/** The member enters a fresh challenge. */
async function ownEntry(
	seed: Seeder,
	me: Account,
	settings: ChallengeSettings,
	options: { blueprint?: string; analysed?: boolean } = {},
) {
	const challenge = newChallenge(settings);
	const entry = await entered(seed, me, challenge, options);
	return { challenge, entry };
}

test.describe("as an entrant", () => {
	test.use({ viewer: "member" });

	test("reads its own score, or why its entry is not ranked, in its entry section", async ({
		page,
		seed,
		me,
	}) => {
		// Waiting for the analysis, then scored.
		const waiting = await ownEntry(seed, me, {}, { analysed: false });
		await open(page, waiting.challenge);
		await expect(yourEntry(page)).toContainText("Waiting for the analysis.");
		storeAnalysis(waiting.entry.id, { width: 4, height: 4 });
		await page.reload();
		await expect(yourEntry(page)).toContainText("Score: 16 tiles");

		// A rule the analysed entry breaks.
		const tooBig = await ownEntry(
			seed,
			me,
			{ rules: { restrictions: [{ param: "footprint_area", op: "<", value: 10 }] } },
			{ analysed: false },
		);
		storeAnalysis(tooBig.entry.id, { width: 12, height: 7 });
		await open(page, tooBig.challenge);
		await expectRejected(page, "Footprint area is 84 tiles; it must be under 10 tiles");
		// Still listed for its entrant, with the card, and counted for no one.
		await expect(entryOf(page, tooBig.entry)).toHaveCount(1);
		await expect(entriesHeading(page)).toHaveText("Entries (0)");

		// A rule the blueprint's own entities already break, before any analysis.
		const noAssemblers = await ownEntry(
			seed,
			me,
			{
				rules: {
					restrictions: [
						{
							param: "entity_count",
							target: { name: "assembling-machine-3" },
							op: "=",
							value: 0,
						},
					],
				},
			},
			{ analysed: false },
		);
		await open(page, noAssemblers.challenge);
		await expectRejected(page, /Entity count of .* is 6; it must be exactly 0/);

		// A failed and a partial analysis.
		const failed = await ownEntry(seed, me, {}, { analysed: false });
		failAnalysis(failed.entry.id);
		await open(page, failed.challenge);
		await expectRejected(page, "the analysis failed");
		const partial = await ownEntry(seed, me, {}, { analysed: false });
		storeAnalysis(partial.entry.id, { width: 1, height: 1, partial: true });
		await open(page, partial.challenge);
		await expectRejected(
			page,
			"the analysis is incomplete: some entities could not be calculated",
		);

		// Staff disqualify it with a reason.
		const disqualified = await ownEntry(seed, me, {});
		overrideChallengeEntry(
			disqualified.challenge.slug,
			disqualified.entry.id,
			"rejected",
			"Copied",
		);
		await open(page, disqualified.challenge);
		await expectRejected(page, "Copied");

		// The kind and content rules, and the revision rule.
		const platform = await ownEntry(seed, me, { rules: { kind: "platform" } });
		await open(page, platform.challenge);
		await expectRejected(page, "the entry must be a space platform");
		const vanilla = await ownEntry(
			seed,
			me,
			{ rules: { content: "vanilla" } },
			{ blueprint: SPACE_AGE_20 },
		);
		await open(page, vanilla.challenge);
		await expectRejected(page, "uses Space Age content; this challenge is vanilla only");
		const stale = await ownEntry(seed, me, {});
		ageRevisions(stale.entry.id);
		await open(page, stale.challenge);
		await expectRejected(
			page,
			"the blueprint was uploaded before the challenge started; upload a new revision",
		);
		await seed.revision(stale.entry, relabel(BLUEPRINT_21, `New ${uniqueWord()}`), me);
		await page.reload();
		await expect(yourEntry(page)).toContainText("Waiting for the analysis.");
	});
});

test.describe("rules", () => {
	test("an entity count restriction rejects before the analysis, a footprint one after it", async ({
		page,
		seed,
	}) => {
		const noAssemblers = newChallenge({
			rules: {
				restrictions: [
					{
						param: "entity_count",
						target: { name: "assembling-machine-3" },
						op: "=",
						value: 0,
					},
				],
			},
		});
		const blocked = await entered(seed, await seed.account(), noAssemblers);
		await open(page, noAssemblers);
		await expect(page.getByText(/Entity count of .*: exactly 0/)).toBeVisible();
		// No analysis yet: the blueprint's own entities already break the rule.
		await expectNotPublic(page, blocked);

		const compact = newChallenge({
			rules: { restrictions: [{ param: "footprint_area", op: "<", value: 60 }] },
		});
		const owner = await seed.account();
		const fits = await entered(seed, owner, compact);
		const tooBig = await entered(seed, await seed.account(), compact);
		storeAnalysis(fits.id, { width: 10, height: 5 });
		storeAnalysis(tooBig.id, { width: 12, height: 7 });
		await open(page, compact);
		await expect(page.getByText("Footprint area: under 60 tiles")).toBeVisible();
		await expect(entryOf(page, fits)).toContainText("Score: 50 tiles");
		await expectNotPublic(page, tooBig);
	});

	test("a vanilla challenge refuses Space Age content; any game version ranks", async ({
		page,
		seed,
	}) => {
		const vanilla = newChallenge({ rules: { content: "vanilla" } });
		// A Factorio 2.0 blueprint with biochambers and a recycler.
		const spaceAge = await entered(seed, await seed.account(), vanilla, {
			blueprint: SPACE_AGE_20,
		});
		await open(page, vanilla);
		await expect(page.getByText("Base game only: no Space Age content, no mods")).toBeVisible();
		await expectNotPublic(page, spaceAge);

		// The same 2.0 blueprint ranks in a Space Age challenge.
		const open21 = newChallenge();
		const older = await entered(seed, await seed.account(), open21, {
			blueprint: SPACE_AGE_20,
		});
		storeAnalysis(older.id, { width: 6, height: 6 });
		await open(page, open21);
		await expect(entryOf(page, older)).toContainText("Score: 36 tiles");
	});

	test("the kind rule: a platform challenge refuses a factory, a factory one takes it", async ({
		page,
		seed,
	}) => {
		const platforms = newChallenge({ rules: { kind: "platform" } });
		const factory = await entered(seed, await seed.account(), platforms);
		storeAnalysis(factory.id, { width: 4, height: 4 });
		await open(page, platforms);
		await expect(page.getByText("A space platform", { exact: true })).toBeVisible();
		await expectNotPublic(page, factory);

		const factories = newChallenge({ rules: { kind: "factory" } });
		const accepted = await entered(seed, await seed.account(), factories);
		storeAnalysis(accepted.id, { width: 4, height: 4 });
		await open(page, factories);
		await expect(page.getByText("Not a space platform")).toBeVisible();
		await expect(entryOf(page, accepted)).toContainText("Score: 16 tiles");
	});

	test("only revisions uploaded after the start count, when the challenge says so", async ({
		page,
		seed,
	}) => {
		const strict = newChallenge();
		const owner = await seed.account();
		const old = await entered(seed, owner, strict);
		storeAnalysis(old.id, { width: 3, height: 3 });
		ageRevisions(old.id);
		await open(page, strict);
		await expectNotPublic(page, old);

		// A new revision makes it count, but it waits for its own analysis: not public yet.
		await seed.revision(old, relabel(BLUEPRINT_21, `New ${uniqueWord()}`), owner);
		await page.reload();
		await expectNotPublic(page, old);

		const lenient = newChallenge({ requireNewRevisions: false });
		const other = await seed.account();
		const older = await entered(seed, other, lenient);
		storeAnalysis(older.id, { width: 3, height: 3 });
		ageRevisions(older.id);
		await open(page, lenient);
		await expect(page.getByText(/Uploaded after the challenge started/)).toBeHidden();
		await expect(entryOf(page, older)).toContainText("Score: 9 tiles");
	});
});

test("an ended challenge puts its top three by score and by vote on podiums of their own", async ({
	page,
	seed,
}) => {
	const challenge = newChallenge();
	const made: SeededEntry[] = [];
	for (const [index, owner] of (await accounts(seed, 4)).entries())
		made.push(
			await entered(seed, owner, challenge, { title: `Place ${index + 1} ${uniqueWord()}` }),
		);
	const [first, second, third, fourth] = made;
	storeAnalysis(first.id, { width: 5, height: 5 });
	storeAnalysis(second.id, { width: 6, height: 6 });
	storeAnalysis(third.id, { width: 7, height: 7 });
	storeAnalysis(fourth.id, { width: 8, height: 8 });
	const [voterA, voterB] = await accounts(seed, 2);
	await seed.challengeVote(voterA, challenge.slug, fourth);
	await seed.challengeVote(voterB, challenge.slug, fourth);
	await seed.challengeVote(voterA, challenge.slug, third);

	// No podium while the challenge runs.
	await open(page, challenge);
	await expect(page.getByRole("region", { name: "Winners" })).toHaveCount(0);

	setChallengePhase(challenge.slug, "closed");
	await page.reload();
	const winners = page.getByRole("region", { name: "Winners" });
	await expect(place(winners, "1st place")).toContainText(first.title);
	await expect(place(winners, "1st place")).toContainText("25 tiles");
	await expect(place(winners, "2nd place")).toContainText(second.title);
	await expect(place(winners, "2nd place")).toContainText("36 tiles");
	await expect(place(winners, "3rd place")).toContainText(third.title);
	await expect(place(winners, "3rd place")).toContainText("49 tiles");

	// The rest follow under their own heading; podium entries are not repeated.
	await expect(page.getByRole("heading", { name: "Other entries" })).toBeVisible();
	await expect(entryOf(page, fourth).getByLabel("Rank 4")).toBeVisible();
	await expect(entryOf(page, first)).toHaveCount(0);

	const community = page.getByRole("region", { name: "Community vote" });
	await expect(place(community, "1st place")).toContainText(fourth.title);
	await expect(place(community, "1st place")).toContainText("2 votes");
	await expect(place(community, "2nd place")).toContainText(third.title);
	await expect(place(community, "2nd place")).toContainText("1 vote");
	// Nobody else got a vote: the third step stays empty.
	await expect(place(community, "3rd place")).toHaveCount(0);
});
