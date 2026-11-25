// Changing an entry's visibility from its edit form, between all three
// settings, and what everyone else then sees: the entry by link, the owner's
// public profile and search.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { expect, test } from "../support/fixtures.ts";
import { webURL } from "../support/stack.ts";
import { entryDetails, ready } from "../support/ui.ts";

type Visibility = "public" | "unlisted" | "private";

const options: Record<Visibility, string> = {
	public: "Public (listed and searchable)",
	unlisted: "Unlisted (anyone with the link)",
	private: "Private (only you)",
};
const notes: Record<Visibility, string | undefined> = {
	public: undefined,
	unlisted: "Unlisted: only people with the link can see it",
	private: "Private: only you can see it",
};

async function changeVisibility(page: Page, to: Visibility): Promise<void> {
	await ready(page);
	await page.getByRole("button", { name: "Edit details" }).click();
	await page.getByLabel("Visibility").selectOption(options[to]);
	await Promise.all([
		page.waitForEvent("load"),
		page.getByRole("button", { name: "Save" }).click(),
	]);
}

test.describe("as the owner", () => {
	test.use({ viewer: "member" });

	const all: Visibility[] = ["public", "unlisted", "private"];
	for (const from of all) {
		for (const to of all) {
			if (from === to) continue;

			test(`changes ${from} to ${to}`, async ({ page, browser, seed, me }) => {
				const word = uniqueWord();
				const entry = await seed.entry({
					title: `Vis ${word}`,
					owner: me,
					visibility: from,
				});
				// A public twin shows the profile and the index are up to date.
				const twin = await seed.entry({ title: `Twin ${word}`, owner: me });
				if (from === "public") await seed.searchable(entry, word);

				await page.goto(`/e/${entry.id}`);
				await changeVisibility(page, to);
				const note = notes[to];
				if (note) await expect(entryDetails(page)).toContainText(note);
				else await expect(entryDetails(page)).not.toContainText("Visibility");
				await ready(page);
				await page.getByRole("button", { name: "Edit details" }).click();
				await expect(page.getByLabel("Visibility")).toHaveValue(to);

				if (to === "public") await seed.searchable(entry, word);
				else await seed.searchable(twin, word);

				const visitor = await browser.newPage({ baseURL: webURL });
				try {
					await visitor.goto(`/e/${entry.id}`);
					const heading = to === "private" ? "Not found" : entry.title;
					await expect(
						visitor.getByRole("heading", { level: 1, name: heading }),
					).toBeVisible();
					await expect(
						visitor.getByRole("button", { name: "Edit details" }),
					).toBeHidden();

					await visitor.goto(`/u/${me.username}`);
					await expect(
						visitor.getByRole("heading", { level: 2, name: twin.title }),
					).toBeVisible();
					const onProfile = visitor.getByRole("heading", { level: 2, name: entry.title });
					if (to === "public") await expect(onProfile).toBeVisible();
					else await expect(onProfile).toBeHidden();

					await visitor.goto(`/browse?q=${encodeURIComponent(word)}`);
					await expect(
						visitor.getByRole("heading", { level: 2, name: twin.title }),
					).toBeVisible();
					const inSearch = visitor.getByRole("heading", { level: 2, name: entry.title });
					if (to === "public") await expect(inSearch).toBeVisible();
					else await expect(inSearch).toBeHidden();
				} finally {
					await visitor.close();
				}
			});
		}
	}
});

test("an anonymous entry's edit form offers no visibility", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Anon ${uniqueWord()}` });
	await page.goto(`/e/${entry.id}?token=${entry.editToken}`);
	await ready(page);
	await page.getByRole("button", { name: "Edit details" }).click();
	await expect(page.getByLabel("Entry title")).toBeVisible();
	await expect(page.getByLabel("Visibility")).toBeHidden();
});
