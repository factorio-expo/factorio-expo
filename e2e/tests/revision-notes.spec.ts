// The optional note on a revision: written on the new-revision form (by the
// owner or an edit-link holder), shown in the revision history, never on the
// owner's public profile.
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, OTHER_BLUEPRINT, relabel } from "../support/blueprints.ts";
import { expect, test, type Viewer } from "../support/fixtures.ts";
import { revisionButton, submitRevision } from "../support/ui.ts";

const editors: { name: string; viewer: Viewer }[] = [
	{ name: "with the edit link", viewer: "guest" },
	{ name: "as the owner", viewer: "member" },
];

for (const editor of editors) {
	test.describe(editor.name, () => {
		test.use({ viewer: editor.viewer });

		test("adds a note to a revision and sees it in the revision history", async ({
			page,
			seed,
			account,
		}) => {
			const entry = await seed.entry({ owner: account });
			const url = entry.editToken
				? `/e/${entry.id}?token=${entry.editToken}`
				: `/e/${entry.id}`;
			const note = `Moved the inserters ${uniqueWord()}`;

			await page.goto(url);
			await page.getByRole("button", { name: "New revision" }).click();
			await submitRevision(page, relabel(OTHER_BLUEPRINT, uniqueWord()), `  ${note}  `);

			await expect(revisionButton(page)).toHaveText("Revision 2");
			await revisionButton(page).click();
			const dialog = page.getByRole("dialog", { name: "Pick a revision" });
			const revisions = dialog.getByRole("listitem");
			await expect(revisions).toHaveCount(2);
			// The newest first, with its note; the first revision has none.
			await expect(revisions.first()).toContainText("Revision 2 (current)");
			await expect(revisions.first()).toContainText(note);
			await expect(revisions.last()).toContainText("Revision 1");
			await expect(revisions.last().locator(".revision-note")).toHaveCount(0);
		});
	});
}

test.describe("as member", () => {
	test.use({ viewer: "member" });

	test("limits a note to 280 characters", async ({ page, seed, me }) => {
		const entry = await seed.entry({ owner: me });
		await page.goto(`/e/${entry.id}/revisions/new`);
		const note = page.getByLabel(/^\s*What changed\?/);
		await note.fill("x".repeat(300));
		await expect(note).toHaveValue("x".repeat(280));
	});

	test("an API note over the limit is refused with its own code", async ({ seed, me }) => {
		const entry = await seed.entry({ owner: me });
		const refused = await me.api.post(`/api/v1/entries/${entry.id}/revisions`, {
			data: { blueprint: BLUEPRINT, note: "x".repeat(281) },
		});
		expect(refused.status()).toBe(400);
		expect(((await refused.json()) as { code: string }).code).toBe("invalid_revision_note");
	});

	test("a note shows in the history, but not on the profile", async ({ page, seed, me }) => {
		const entry = await seed.entry({ owner: me });
		const note = `Secret sauce ${uniqueWord()}`;
		await seed.revision(entry, relabel(OTHER_BLUEPRINT, uniqueWord()), me, note);

		await page.goto(`/e/${entry.id}`);
		await revisionButton(page).click();
		await expect(page.getByRole("dialog", { name: "Pick a revision" })).toContainText(note);

		await page.goto(`/u/${me.username}`);
		await expect(page.getByRole("heading", { level: 2, name: entry.title })).toBeVisible();
		await expect(page.getByText(note)).toBeHidden();
	});
});
