import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BOOK, OTHER_BLUEPRINT, relabel } from "../support/blueprints.ts";
import { expect, test, viewers } from "../support/fixtures.ts";
import { bookLeaf, pickRevision, revisionButton } from "../support/ui.ts";

const olderRevisionNotice = "You are viewing an older revision.";

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("switches between blueprint revisions", async ({ page, seed }) => {
			const before = `Before ${uniqueWord()}`;
			const after = `After ${uniqueWord()}`;
			const entry = await seed.entry({ blueprint: relabel(BLUEPRINT, before) });
			await seed.revision(entry, relabel(OTHER_BLUEPRINT, after));

			await page.goto(`/e/${entry.id}`);
			await expect(page.getByRole("heading", { level: 2, name: after })).toBeVisible();
			await expect(revisionButton(page)).toHaveText("Revision 2");
			await expect(page.getByText(olderRevisionNotice)).toBeHidden();

			await pickRevision(page, "Revision 1");
			await expect(page).toHaveURL(/[?&]revision=/);
			await expect(page.getByText(olderRevisionNotice)).toBeVisible();
			await expect(page.getByRole("heading", { level: 2, name: before })).toBeVisible();
			// Title and description belong to the entry, not the revision.
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();

			await pickRevision(page, "Revision 2 (current)");
			await expect(page).toHaveURL(`/e/${entry.id}`);
			await expect(page.getByText(olderRevisionNotice)).toBeHidden();
			await expect(page.getByRole("heading", { level: 2, name: after })).toBeVisible();
		});

		test("switches between book revisions and opens a blueprint of the old one", async ({
			page,
			seed,
		}) => {
			const before = `Old book ${uniqueWord()}`;
			const after = `New book ${uniqueWord()}`;
			const entry = await seed.entry({ blueprint: relabel(BOOK, before) });
			await seed.revision(entry, relabel(BOOK, after));

			await page.goto(`/e/${entry.id}`);
			await expect(page.locator(".tree").getByText(after)).toBeVisible();

			await pickRevision(page, "Revision 1");
			await expect(page.getByText(olderRevisionNotice)).toBeVisible();
			await expect(page.locator(".tree").getByText(before)).toBeVisible();

			await (await bookLeaf(page, "Chemical plant cell")).click();
			await expect(
				page.getByRole("heading", { level: 1, name: "Chemical plant cell" }),
			).toBeVisible();
			await page.getByRole("link", { name: "← Back to entry" }).click();
			await expect(page.getByText(olderRevisionNotice)).toBeVisible();
			await expect(page.locator(".tree").getByText(before)).toBeVisible();
		});

		test("lists every revision with the current one marked", async ({ page, seed }) => {
			const entry = await seed.entry();
			await seed.revision(entry, OTHER_BLUEPRINT);
			await seed.revision(entry, BLUEPRINT);

			await page.goto(`/e/${entry.id}`);
			await revisionButton(page).click();
			const dialog = page.getByRole("dialog", { name: "Pick a revision" });
			await expect(dialog.getByRole("link")).toHaveText([
				"Revision 3 (current)",
				"Revision 2",
				"Revision 1",
			]);
			await dialog.getByRole("button", { name: "Close revision history" }).click();
			await expect(dialog).toBeHidden();
		});
	});
}
