import { uniqueWord } from "../support/api.ts";
import {
	BLUEPRINT,
	BOOK,
	decode,
	encode,
	relabel,
	SPACE_AGE_20,
	withEntity,
} from "../support/blueprints.ts";
import { expect, test, viewers } from "../support/fixtures.ts";
import { bookLeaf, openExchangeString, revisionButton } from "../support/ui.ts";

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("views a blueprint", async ({ page, seed }) => {
			const label = `Layout ${uniqueWord()}`;
			const entry = await seed.entry({
				title: `Blueprint ${uniqueWord()}`,
				description: "Feeds **two** lines.",
				blueprint: relabel(SPACE_AGE_20, label),
				tags: ["smelting"],
			});

			await page.goto(`/e/${entry.id}`);
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
			await expect(page.locator(".entry-description strong")).toHaveText("two");
			await expect(page.getByRole("list", { name: "Entry tags" })).toContainText(/smelting/i);
			await expect(page.getByTitle("Uploader")).toHaveText("Anonymous");
			await expect(page.getByTitle("Game version")).toHaveText(/^\s*2\.0\.\d+\s*$/);
			await expect(page.getByRole("heading", { level: 2, name: label })).toBeVisible();
			await expect(page.getByRole("button", { name: "Copy string" })).toBeVisible();
			await expect(await openExchangeString(page, "Blueprint")).toHaveValue(/^0e/);
			await expect(page.getByRole("heading", { name: /^Entities/ })).toBeVisible();
			// Recipes show how many entities are set to each, like entities.
			await expect(
				page
					.getByRole("img", { name: "fish-breeding", exact: true })
					.locator(".chip-badge"),
			).toHaveText("1");
			await expect(revisionButton(page)).toHaveText("Revision 1");
			// Its recyclers and fish breeding are built-in mods: exact matches.
			const mods = page.getByRole("region", { name: "Mods" });
			await expect(
				mods.getByRole("list", { name: "Needed mods" }).getByRole("listitem"),
			).toHaveText(["Recycler", "Space Age"]);
			await expect(mods).not.toContainText("leave out");
			// The editor knows the game's own mods: no warning on its link.
			await expect(page.getByRole("button", { name: "Open in editor" })).not.toHaveAttribute(
				"aria-describedby",
			);
			// Size and grid sit in the details panel; the footprint waits for the analysis.
			const facts = page.getByRole("region", { name: "Entry details" });
			await expect(facts.locator("[data-footprint]")).toHaveText("Measuring…");
			await expect(facts.getByRole("region", { name: "Mods" })).toBeVisible();
			await expect(facts.locator("[data-snap-to-grid]")).toHaveText("Off");
			// The exchange string is ASCII: its characters are its bytes.
			await expect(facts.locator("[data-string-size]")).toHaveText(
				/^[\d,]+ characters \([\d.]+ k?B\)$/,
			);
		});

		test("renders a blueprint's rich text", async ({ page, seed }) => {
			const word = uniqueWord();
			const node = decode(BLUEPRINT);
			node.blueprint!.label = `[space-age]${word} [color=red]red[/color] [gps=1,2]`;
			node.blueprint!.description = "First line\n[font=default-bold]bold[/font] line";
			// An entry title of its own: the blueprint keeps its heading.
			const entry = await seed.entry({
				title: `Entry ${word}`,
				blueprint: encode(node),
			});

			await page.goto(`/e/${entry.id}`);
			const heading = page.getByRole("heading", { level: 2, name: `${word} red [gps=1,2]` });
			await expect(heading).toBeVisible();
			await expect(heading.getByRole("img", { name: "Space Age" })).toBeVisible();
			await expect(heading.locator(".rich-text-color")).toHaveText("red");
			const description = page.locator("article header p");
			await expect(description.locator("br")).toHaveCount(1);
			await expect(description.locator("strong")).toHaveText("bold");
		});

		test("does not repeat the entry's own title and description", async ({ page, seed }) => {
			const word = uniqueWord();
			const node = decode(BLUEPRINT);
			node.blueprint!.label = `Same ${word}`;
			node.blueprint!.description = `Same text ${word}`;
			const entry = await seed.entry({
				title: node.blueprint!.label,
				description: node.blueprint!.description,
				blueprint: encode(node),
			});

			await page.goto(`/e/${entry.id}`);
			await expect(
				page.getByRole("heading", { level: 1, name: `Same ${word}` }),
			).toBeVisible();
			await expect(page.getByRole("heading", { level: 2, name: `Same ${word}` })).toHaveCount(
				0,
			);
			await expect(page.getByText(`Same text ${word}`)).toHaveCount(1);
		});

		test("names a blueprint's unknown mod", async ({ page, seed }) => {
			const entry = await seed.entry({
				title: `Modded ${uniqueWord()}`,
				blueprint: withEntity(SPACE_AGE_20, "e2e-mystery-machine"),
			});

			await page.goto(`/e/${entry.id}`);
			const mods = page.getByRole("region", { name: "Mods" });
			await expect(
				mods.getByRole("list", { name: "Needed mods" }).getByRole("listitem"),
			).toHaveText(["Recycler", "Space Age", "unknown mod"]);
			await expect(mods).toContainText(
				"The preview and analysis leave out what the other mods add.",
			);
			await mods.getByText(/Names no listed mod defines/).click();
			await expect(
				mods.getByRole("list", { name: "Unknown names" }).getByRole("listitem"),
			).toHaveText(["e2e-mystery-machine"]);
			// The editor link stays but warns what it will leave out.
			const editor = page.getByRole("button", { name: "Open in editor" });
			const warning =
				"This blueprint uses mods the editor does not support (unknown mod), so their entities will be missing there.";
			await expect(editor).toHaveAccessibleDescription(warning);
			await expect(editor).toHaveAttribute("data-tooltip", `Open in editor. ${warning}`);
		});

		test("a tag on the entry opens the browse page filtered by it", async ({ page, seed }) => {
			const entry = await seed.entry({
				title: `Tagged ${uniqueWord()}`,
				tags: ["smelting", "mid-game"],
			});

			await page.goto(`/e/${entry.id}`);
			await page
				.getByRole("list", { name: "Entry tags" })
				.getByRole("link", { name: "Smelting" })
				.click();

			await expect(page).toHaveURL(/\/browse\?tags=smelting$/);
			await page.getByText("Advanced search").click();
			await expect(page.locator('input[name="tags"][value="smelting"]')).toBeChecked();
		});

		test("views a book and the blueprints inside it", async ({ page, seed }) => {
			const entry = await seed.entry({ title: `Book ${uniqueWord()}`, blueprint: BOOK });

			await page.goto(`/e/${entry.id}`);
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
			await expect(await openExchangeString(page, "Book")).toHaveValue(/^0e/);
			const tree = page.locator(".tree");
			await expect(tree.getByText("Parametric crafts")).toBeVisible();
			await expect(tree.getByText("Assemblers")).toBeVisible();
			await expect(tree.getByText("Fluids")).toBeVisible();
			// Only the root book starts open; its sub-books' blueprints wait for a click.
			await expect(tree.getByRole("link", { name: "Assembler 2 cell" })).toBeHidden();
			await (await bookLeaf(page, "Assembler 2 cell")).click();
			await expect(page).toHaveURL(new RegExp(`/e/${entry.id}/b/[0-9A-Za-z]{12}`));
			await expect(
				page.getByRole("heading", { level: 1, name: "Assembler 2 cell" }),
			).toBeVisible();
			await expect(await openExchangeString(page, "Blueprint")).toHaveValue(/^0e/);

			await page.getByRole("link", { name: "← Back to entry" }).click();
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
			// Back on the entry, sub-books start closed again; every blueprint is a card.
			await expect(await bookLeaf(page, "Assembler 2 cell")).toBeVisible();
			await expect(tree.getByRole("link")).toHaveCount(10);
		});

		test("cannot edit someone else's entry", async ({ page, seed }) => {
			const owner = await seed.account();
			const entry = await seed.entry({ title: `Theirs ${uniqueWord()}`, owner });

			await page.goto(`/e/${entry.id}`);
			await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
			await expect(page.getByTitle("Uploader")).toHaveText(owner.username);
			await expect(page.getByRole("button", { name: "Edit details" })).toBeHidden();
			await expect(page.getByRole("button", { name: "New revision" })).toBeHidden();
		});

		test("a private entry stays hidden from everyone but its owner", async ({ page, seed }) => {
			const owner = await seed.account();
			const entry = await seed.entry({
				title: `Secret ${uniqueWord()}`,
				owner,
				visibility: "private",
			});

			await page.goto(`/e/${entry.id}`);
			await expect(page.getByRole("heading", { level: 1, name: "Not found" })).toBeVisible();
		});

		test("an unknown entry shows the not-found page", async ({ page }) => {
			await page.goto("/e/000000000000");
			await expect(page.getByRole("heading", { level: 1, name: "Not found" })).toBeVisible();
			await page.getByRole("link", { name: "Back home" }).click();
			await expect(page).toHaveURL("/");
		});
	});
}

test("a guest sees favorite counts but no favorite or collection buttons", async ({
	page,
	seed,
}) => {
	const entry = await seed.entry();
	await page.goto(`/e/${entry.id}`);
	await expect(page.getByRole("heading", { level: 1, name: entry.title })).toBeVisible();
	await expect(page.getByRole("button", { name: "Add to favorites" })).toBeHidden();
	await expect(page.getByRole("button", { name: "Add to collection" })).toBeHidden();
});

test.describe("as a member", () => {
	test.use({ viewer: "member" });

	test("sees favorite and collection buttons on entry and blueprint pages", async ({
		page,
		seed,
	}) => {
		const entry = await seed.entry({ blueprint: BOOK });
		await page.goto(`/e/${entry.id}`);
		await expect(page.getByRole("button", { name: "Add to favorites" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Add to collection" })).toBeVisible();

		await (await bookLeaf(page, "Foundry cell")).click();
		await expect(page.getByRole("heading", { level: 1, name: "Foundry cell" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Add to favorites" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Add to collection" })).toBeVisible();
	});
});
