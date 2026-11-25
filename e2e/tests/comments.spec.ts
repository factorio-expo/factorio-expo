// The comment thread under a public entry: who may post, Markdown with icons
// and #N references, the revision a comment refers to, editing, removal by the author, the owner and the edit
// link, reporting, the leaf page's link to the thread, and the thread's
// deletion when the entry stops being public.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BOOK } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { bookLeaf, pickRevision, ready } from "../support/ui.ts";

const thread = (page: Page) => page.locator("#comments");
const comment = (page: Page, number: number) => page.locator(`#c-${number}`);

async function post(page: Page, body: string): Promise<void> {
	await ready(page);
	await thread(page)
		.getByLabel(/^\s*Add a comment/)
		.fill(body);
	await thread(page).getByRole("button", { name: "Post comment" }).click();
}

test("a guest reads the thread and is asked to log in", async ({ page, seed }) => {
	const author = await seed.account();
	const entry = await seed.entry({ title: `Talk ${uniqueWord()}` });
	await seed.comment(author, entry, "Uses **lots** of [item=iron-ore]");

	await page.goto(`/e/${entry.id}`);
	await expect(thread(page).getByRole("heading", { name: /Comments/ })).toContainText("(1)");
	await expect(comment(page, 1).locator("strong")).toHaveText("lots");
	await expect(comment(page, 1).getByRole("img", { name: "item=iron-ore" })).toBeVisible();
	await expect(comment(page, 1).getByRole("link", { name: author.username })).toBeVisible();
	await expect(thread(page).getByRole("link", { name: "Log in" })).toBeVisible();
	await expect(thread(page).getByLabel(/^\s*Add a comment/)).toHaveCount(0);
	await expect(comment(page, 1).getByRole("button", { name: /Report/ })).toHaveCount(0);
});

test.describe("as a member", () => {
	test.use({ viewer: "member" });

	test("posts, references, edits and removes a comment", async ({ page, seed, me }) => {
		const other = await seed.account();
		const entry = await seed.entry({ title: `Talk ${uniqueWord()}` });
		await seed.comment(other, entry, "First!");
		await page.goto(`/e/${entry.id}`);

		await post(page, "Agree with #1, and #9 does not exist. <script>alert(1)</script>");
		await expect(comment(page, 2)).toContainText(me.username);
		await expect(comment(page, 2)).toContainText("Agree with #1");
		await expect(
			comment(page, 2).getByRole("link", { name: "#1", exact: true }),
		).toHaveAttribute("href", "#c-1");
		await expect(comment(page, 2).getByRole("link", { name: "#9" })).toHaveCount(0);
		await expect(comment(page, 2).locator("script")).toHaveCount(0);
		// Others' comments can only be reported; one's own edited and removed.
		await expect(comment(page, 1).getByRole("button", { name: "Edit comment #1" })).toHaveCount(
			0,
		);

		await comment(page, 2).getByRole("button", { name: "Edit comment #2" }).click();
		await comment(page, 2).getByLabel("Text of comment #2").fill("Changed my mind.");
		await comment(page, 2).getByRole("button", { name: "Save" }).click();
		await expect(comment(page, 2)).toContainText("Changed my mind.");
		await expect(comment(page, 2)).toContainText("edited");

		page.once("dialog", (dialog) => void dialog.accept());
		await comment(page, 2).getByRole("button", { name: "Remove comment #2" }).click();
		await expect(comment(page, 2)).toContainText("Removed by its author.");
		await expect(comment(page, 2)).not.toContainText("Changed my mind.");
		await page.reload();
		await expect(comment(page, 2)).toContainText("Removed by its author.");
	});

	test("a comment refers to the revision picked", async ({ page, seed, me }) => {
		const entry = await seed.entry({ title: `Revised ${uniqueWord()}`, owner: me });
		await seed.revision(entry, BLUEPRINT, me);
		await page.goto(`/e/${entry.id}`);
		await post(page, "On the current one");
		await expect(comment(page, 1)).toContainText("rev 2");

		await pickRevision(page, "Revision 1");
		await post(page, "On the first one");
		await expect(comment(page, 2)).toContainText("rev 1");
	});

	test("reports someone else's comment", async ({ page, seed }) => {
		const other = await seed.account();
		const entry = await seed.entry({ title: `Talk ${uniqueWord()}` });
		await seed.comment(other, entry, "Buy cheap gold");
		await page.goto(`/e/${entry.id}`);
		await ready(page);

		await comment(page, 1).getByRole("button", { name: "Report comment #1" }).click();
		const dialog = page.getByRole("dialog", { name: "Report comment #1" });
		await dialog.getByLabel("Spam").check();
		await dialog.getByRole("button", { name: "Send report" }).click();
		await expect(dialog).toBeHidden();
		await expect(comment(page, 1)).toContainText("a moderator will look at it");
	});

	test("the owner removes a comment and closing the entry deletes the thread", async ({
		page,
		seed,
		me,
	}) => {
		const other = await seed.account();
		const entry = await seed.entry({ title: `Mine ${uniqueWord()}`, owner: me });
		await seed.comment(other, entry, "Rude remark");
		await seed.comment(other, entry, "Fair point");
		await page.goto(`/e/${entry.id}`);
		await ready(page);

		page.once("dialog", (dialog) => void dialog.accept());
		await comment(page, 1).getByRole("button", { name: "Remove comment #1" }).click();
		await expect(comment(page, 1)).toContainText("Removed by the entry owner.");

		await page.getByRole("button", { name: "Edit details" }).click();
		await page.getByLabel("Visibility").selectOption("Unlisted (anyone with the link)");
		let warning = "";
		page.once("dialog", (dialog) => {
			warning = dialog.message();
			void dialog.accept();
		});
		await Promise.all([
			page.waitForEvent("load"),
			page.getByRole("button", { name: "Save" }).click(),
		]);
		expect(warning).toContain("deletes its 2 comments");
		await expect(thread(page)).toContainText("Comments are only open on public entries.");
		await expect(comment(page, 2)).toHaveCount(0);
	});
});

test("the edit link removes comments on an anonymous entry", async ({ page, seed }) => {
	const other = await seed.account();
	const entry = await seed.entry({ title: `Anon ${uniqueWord()}` });
	await seed.comment(other, entry, "Off topic");
	await page.goto(`/e/${entry.id}?token=${entry.editToken}`);
	await ready(page);

	page.once("dialog", (dialog) => void dialog.accept());
	await comment(page, 1).getByRole("button", { name: "Remove comment #1" }).click();
	await expect(comment(page, 1)).toContainText("Removed by the entry owner.");
});

test("a leaf page links to the entry's thread", async ({ page, seed }) => {
	const other = await seed.account();
	const entry = await seed.entry({ title: `Book ${uniqueWord()}`, blueprint: BOOK });
	await seed.comment(other, entry, "Nice book");
	await page.goto(`/e/${entry.id}`);
	await (await bookLeaf(page, "Biochamber cell")).click();

	await page.getByRole("link", { name: "Comments (1)" }).click();
	await expect(page).toHaveURL(new RegExp(`/e/${entry.id}#comments$`));
	await expect(comment(page, 1)).toContainText("Nice book");
});
