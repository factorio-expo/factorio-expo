// XP and badges on the public profile: an upload earns 10, a favorite from
// someone else 2 (one's own nothing), a comment 1; removed content takes its
// points back. Records and challenge awards are covered by the API's Go tests
// and the leaderboards spec.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { expect, test } from "../support/fixtures.ts";

const progress = (page: Page) => page.locator("[data-profile-progress]");
const badges = (page: Page) => progress(page).getByRole("list", { name: "Badges" });

test("a profile shows the level, XP and badges its activity earned", async ({ page, seed }) => {
	const owner = await seed.account();
	const fan = await seed.account();

	await page.goto(`/u/${owner.username}`);
	await expect(progress(page)).toContainText("Level 1");
	await expect(progress(page)).toContainText("0 XP");
	await expect(progress(page)).toContainText("50 XP to level 2");
	await expect(badges(page)).toHaveCount(0);

	const entry = await seed.entry({ title: `Earner ${uniqueWord()}`, owner });
	await seed.favorite(owner, entry);
	await seed.favorite(fan, entry);
	await seed.comment(owner, entry, "Thanks for looking");
	await page.reload();
	await expect(progress(page)).toContainText("13 XP");
	await expect(progress(page)).toContainText("37 XP to level 2");
	await expect(badges(page).getByRole("listitem")).toHaveText(["First upload", "First favorite"]);
	await expect(page.getByRole("progressbar", { name: "Level 1: 13 XP" })).toHaveAttribute(
		"value",
		"26",
	);

	// Deleting the entry takes back its upload and favorite points (the comment
	// was on it too, but a comment's points stay while the comment does).
	await seed.deleteEntry(entry, owner);
	await page.reload();
	await expect(progress(page)).toContainText("49 XP to level 2");
	await expect(badges(page)).toHaveCount(0);
});
