// What anyone sees of another user: their public profile (status, links,
// numbers, pinned entries, the Entries / Collections / Comments tabs) and
// public collections; and what the owner does there: edit the profile, pin
// and arrange entries.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { expect, test, viewers } from "../support/fixtures.ts";

const pinned = (page: Page) => page.getByRole("region", { name: "Pinned" });
/** The titles of the pinned entries' cards, in order. */
const pinnedTitles = (page: Page) =>
	pinned(page).getByRole("article").getByRole("heading", { level: 2 });
const tabs = (page: Page) => page.getByRole("navigation", { name: "Profile" });

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("browses a user's public collection from their profile", async ({ page, seed }) => {
			const owner = await seed.account();
			const listed = await seed.entry({ title: `Shared ${uniqueWord()}`, owner });
			const secretEntry = await seed.entry({
				title: `Hidden ${uniqueWord()}`,
				owner,
				visibility: "private",
			});
			const shared = await seed.collection(owner, {
				visibility: "public",
				description: "Best builds.",
			});
			const hidden = await seed.collection(owner, { visibility: "private" });
			await seed.addToCollection(owner, shared, listed);
			await seed.addToCollection(owner, shared, secretEntry);
			await seed.favorite(await seed.account(), listed);

			await page.goto(`/u/${owner.username}`);
			await expect(
				page.getByRole("heading", { level: 1, name: owner.username }),
			).toBeVisible();
			await expect(page.getByRole("heading", { level: 2, name: listed.title })).toBeVisible();
			await expect(
				page.getByRole("heading", { level: 2, name: secretEntry.title }),
			).toBeHidden();
			// One public entry, favorited once by someone else.
			await expect(page.getByText("1 public entry")).toBeVisible();
			await expect(page.getByText("1 favorite received")).toBeVisible();

			await tabs(page)
				.getByRole("link", { name: /Collections/ })
				.click();
			await expect(page).toHaveURL(`/u/${owner.username}/collections`);
			// A card with its entry count (what a visitor may see: the private entry is left out).
			const card = page.getByRole("article").filter({ hasText: shared.name });
			await expect(card).toContainText("1 entry");
			await expect(page.getByRole("link", { name: hidden.name })).toBeHidden();

			await card.getByRole("link", { name: shared.name }).click();
			await expect(page).toHaveURL(`/collections/${shared.id}`);
			await expect(page.getByRole("heading", { level: 1, name: shared.name })).toBeVisible();
			await expect(page.getByText(`A collection by ${owner.username}`)).toBeVisible();
			await expect(page.getByText("Best builds.")).toBeVisible();
			await expect(page.getByRole("article")).toHaveCount(1);
			// Listing cards carry the favorites count, like search result cards.
			await expect(page.getByRole("article").locator("[data-favorite]")).toContainText("1");
			await page.getByRole("link", { name: listed.title }).click();
			await expect(page.getByRole("heading", { level: 1, name: listed.title })).toBeVisible();
		});

		test("a private collection is not found", async ({ page, seed }) => {
			const owner = await seed.account();
			const hidden = await seed.collection(owner, { visibility: "private" });
			await page.goto(`/collections/${hidden.id}`);
			await expect(page.getByRole("heading", { level: 1, name: "Not found" })).toBeVisible();
		});

		test("an empty public collection says so", async ({ page, seed }) => {
			const owner = await seed.account();
			const empty = await seed.collection(owner, { visibility: "public" });
			await page.goto(`/collections/${empty.id}`);
			await expect(page.getByText("This collection is empty.")).toBeVisible();
		});

		test("shows the status, links, top tags, pins and comments", async ({ page, seed }) => {
			const owner = await seed.account();
			const word = uniqueWord();
			const first = await seed.entry({ title: `First ${word}`, owner, tags: ["early-game"] });
			const second = await seed.entry({ title: `Second ${word}`, owner });
			const elsewhere = await seed.entry({ title: `Elsewhere ${word}` });
			await seed.comment(owner, elsewhere, `Nice build ${word}`);
			const api = owner.api;
			for (const response of [
				await api.patch("/api/v1/me/profile", {
					data: {
						status: "Building on [planet=gleba]",
						links: [{ platform: "github", handle: "@expo-tester" }],
					},
				}),
				await api.put("/api/v1/me/pins", { data: { entry_ids: [second.id, first.id] } }),
			]) {
				expect(response.ok(), await response.text()).toBe(true);
			}

			await page.goto(`/u/${owner.username}`);
			await expect(page.locator("[data-profile-status]")).toContainText("Building on");
			const github = page.getByRole("link", { name: /GitHub: expo-tester/ });
			await expect(github).toHaveAttribute("href", "https://github.com/expo-tester");
			await expect(github).toHaveAttribute("rel", "nofollow ugc noopener noreferrer me");
			await expect(github).toHaveAttribute("target", "_blank");
			await expect(page.getByRole("list", { name: "Most used tags" })).toContainText(
				"Early game",
			);
			// Pinned in the owner's order; only the owner gets pin toggles.
			await expect(pinnedTitles(page)).toHaveText([second.title, first.title]);
			await expect(page.getByRole("button", { name: /pin/i })).toHaveCount(0);

			await tabs(page)
				.getByRole("link", { name: /Comments/ })
				.click();
			await expect(page).toHaveURL(`/u/${owner.username}/comments`);
			await expect(page.getByText(`Nice build ${word}`)).toBeVisible();
			await page.getByRole("link", { name: new RegExp(elsewhere.title) }).click();
			await expect(page).toHaveURL(new RegExp(`/e/${elsewhere.id}\\?comments_after=0#c-1$`));
			await expect(page.locator("#c-1")).toContainText(`Nice build ${word}`);
		});

		test("an unknown user is not found", async ({ page }) => {
			await page.goto(`/u/${uniqueWord("nobody_")}`);
			await expect(page.getByRole("heading", { level: 1, name: "Not found" })).toBeVisible();
		});
	});
}

test.describe("as the owner", () => {
	test.use({ viewer: "member" });

	test("edits the status and social links", async ({ page, me }) => {
		await page.goto(`/u/${me.username}`);
		await page.getByRole("button", { name: "Edit profile" }).click();
		await page.getByLabel("Status").fill("Megabase on [planet=vulcanus]");
		// A link is refused: only the name goes in.
		await page.getByLabel("GitHub name").fill("https://github.com/someone");
		await page.getByRole("button", { name: "Save", exact: true }).click();
		await expect(page.locator("[data-profile-status-line]")).toContainText("not a GitHub name");

		await page.getByLabel("GitHub name").fill("someone");
		await page.getByLabel("YouTube name").fill("@SomeChannel");
		await page.getByRole("button", { name: "Save", exact: true }).click();
		await expect(page.locator("[data-profile-status]")).toContainText("Megabase on");
		await expect(page.getByRole("link", { name: /YouTube: SomeChannel/ })).toHaveAttribute(
			"href",
			"https://www.youtube.com/@SomeChannel",
		);
		await expect(page.getByRole("link", { name: /GitHub: someone/ })).toBeVisible();
	});

	test("pins entries from the profile, the entry page and My entries, and arranges them", async ({
		page,
		seed,
		me,
	}) => {
		const word = uniqueWord();
		const a = await seed.entry({ title: `Alpha ${word}`, owner: me });
		const b = await seed.entry({ title: `Bravo ${word}`, owner: me });
		const c = await seed.entry({ title: `Charlie ${word}`, owner: me });
		await seed.entry({ title: `Secret ${word}`, owner: me, visibility: "private" });

		await page.goto(`/u/${me.username}`);
		await expect(pinned(page)).toContainText("Nothing pinned yet");
		await expect(page.getByRole("heading", { level: 2, name: /^Entries/ })).toContainText("3");
		// From a card in the entry list.
		const card = page.getByRole("article").filter({ hasText: a.title });
		await card.getByRole("button", { name: "Pin to profile" }).click();
		await expect(pinnedTitles(page)).toHaveText([a.title]);

		// From the entry page.
		await page.goto(`/e/${b.id}`);
		await page.getByRole("button", { name: "Pin to profile" }).click();
		await expect(page.getByRole("button", { name: "Unpin from profile" })).toBeVisible();

		// From My entries: only public entries have the toggle.
		await page.goto("/account/entries");
		const rows = page.getByRole("article");
		await expect(rows).toHaveCount(4);
		await expect(
			rows.filter({ hasText: "Secret" }).getByRole("button", { name: /pin/i }),
		).toHaveCount(0);
		await rows
			.filter({ hasText: c.title })
			.getByRole("button", { name: "Pin to profile" })
			.click();
		await expect(
			rows.filter({ hasText: c.title }).getByRole("button", { name: "Unpin from profile" }),
		).toBeVisible();

		await page.goto(`/u/${me.username}`);
		await expect(pinnedTitles(page)).toHaveText([a.title, b.title, c.title]);

		// Arrange: move Charlie to the top with the keyboard, unpin Bravo.
		await page.getByRole("button", { name: "Arrange" }).click();
		const dialog = page.getByRole("dialog", { name: "Arrange pinned entries" });
		const rowsInDialog = dialog.locator("[data-pin-row]");
		await expect(rowsInDialog).toHaveText([a.title, b.title, c.title]);
		await rowsInDialog.filter({ hasText: c.title }).focus();
		await page.keyboard.press("Space");
		await page.keyboard.press("ArrowUp");
		await page.keyboard.press("ArrowUp");
		await page.keyboard.press("Space");
		await expect(rowsInDialog).toHaveText([c.title, a.title, b.title]);
		await dialog.getByRole("button", { name: `Unpin ${b.title}` }).click();
		await expect(rowsInDialog).toHaveText([c.title, a.title]);
		await dialog.getByRole("button", { name: "Save" }).click();
		await expect(dialog).toBeHidden();
		await expect(pinnedTitles(page)).toHaveText([c.title, a.title]);

		await page.reload();
		await expect(pinnedTitles(page)).toHaveText([c.title, a.title]);
	});

	test("sees their own private entry", async ({ page, seed, me }) => {
		const entry = await seed.entry({
			title: `Private ${uniqueWord()}`,
			owner: me,
			visibility: "private",
		});
		await page.goto(`/e/${entry.id}`);
		await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
		await expect(page.getByRole("button", { name: "Edit details" })).toBeVisible();
	});
});
