// Entry reports: anyone but the entry's editors may flag an entry with a
// reason and a note; "someone else's work" needs the note.
import { uniqueWord } from "../support/api.ts";
import { expect, test } from "../support/fixtures.ts";
import { ready } from "../support/ui.ts";

test("a guest reports an entry as someone else's work", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Copied ${uniqueWord()}` });
	await page.goto(`/e/${entry.id}`);
	await ready(page);

	await page.getByRole("button", { name: "Report entry" }).click();
	const dialog = page.getByRole("dialog", { name: "Report entry" });
	await dialog.getByLabel("Someone else's work, posted without permission").check();
	const note = dialog.getByLabel(/^\s*Details/);
	await expect(note).toHaveAttribute("required", "");
	await expect(dialog).toContainText("Link to the original");
	// The note is required for this reason: the form does not send without it.
	await dialog.getByRole("button", { name: "Send report" }).click();
	await expect(dialog.getByRole("button", { name: "Send report" })).toBeVisible();

	await note.fill("My original: https://example.test/my-build");
	await dialog.getByRole("button", { name: "Send report" }).click();
	await expect(dialog).toContainText("an admin will look at it");
	await dialog.getByRole("button", { name: "Close" }).click();
	await expect(page.getByRole("button", { name: "Entry reported" })).toBeDisabled();
});

test("the anonymous uploader cannot report their own entry", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Mine ${uniqueWord()}` });
	await page.goto(`/e/${entry.id}?token=${entry.editToken}`);
	await ready(page);
	await expect(page.getByRole("button", { name: "Delete entry" })).toBeVisible();
	await expect(page.getByRole("button", { name: "Report entry" })).toHaveCount(0);
});

test.describe("as a member", () => {
	test.use({ viewer: "member" });

	test("reports someone else's entry, not their own", async ({ page, seed, me }) => {
		const other = await seed.entry({ title: `Rude ${uniqueWord()}` });
		await page.goto(`/e/${other.id}`);
		await ready(page);
		await page.getByRole("button", { name: "Report entry" }).click();
		const dialog = page.getByRole("dialog", { name: "Report entry" });
		await dialog.getByLabel("Offensive or inappropriate").check();
		await expect(dialog.getByLabel(/^\s*Details/)).not.toHaveAttribute("required", "");
		await dialog.getByRole("button", { name: "Send report" }).click();
		await expect(dialog).toContainText("an admin will look at it");

		const mine = await seed.entry({ title: `Own ${uniqueWord()}`, owner: me });
		await page.goto(`/e/${mine.id}`);
		await ready(page);
		await expect(page.getByRole("button", { name: "Report entry" })).toHaveCount(0);
	});
});
