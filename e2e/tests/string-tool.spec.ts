import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BOOK, DECONSTRUCTION_BOOK, decode } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { ready } from "../support/ui.ts";

// The blueprint string tool: decode a string to JSON, edit it, encode it back.
test.describe("as guest", () => {
	test("decodes, edits and encodes a book", async ({ page }) => {
		const label = `Edited ${uniqueWord()}`;
		await page.goto("/json");
		await ready(page);
		const exchange = page.getByRole("textbox", { name: "Exchange string" });
		const json = page.getByRole("textbox", { name: "JSON", exact: true });
		const status = page.getByRole("status");

		await exchange.fill(BOOK);
		await page.getByRole("button", { name: "Decode to JSON" }).click();
		await expect(status).toHaveText("Decoded.");
		const decoded = JSON.parse(await json.inputValue());
		expect(decoded).toEqual(decode(BOOK));
		await page.getByRole("tab", { name: "Tree" }).click();
		await expect(page.getByRole("tree", { name: "Blueprint JSON tree" })).toContainText(
			"blueprint_book",
		);

		await page.getByRole("tab", { name: "Edit" }).click();
		decoded.blueprint_book.label = label;
		await json.fill(JSON.stringify(decoded));
		await page.getByRole("button", { name: "Encode to string" }).click();
		await expect(status).toHaveText("Encoded.");
		expect(decode(await exchange.inputValue()).blueprint_book?.label).toBe(label);
	});

	test("encodes JSON written from scratch", async ({ page }) => {
		const label = `Written ${uniqueWord()}`;
		await page.goto("/json");
		await ready(page);
		const exchange = page.getByRole("textbox", { name: "Exchange string" });
		const json = page.getByRole("textbox", { name: "JSON", exact: true });
		const blueprint = decode(BLUEPRINT);
		blueprint.blueprint!.label = label;

		// The JSON field is open first; the tree follows what is typed.
		await json.fill(JSON.stringify(blueprint));
		await page.getByRole("button", { name: "Encode to string" }).click();
		await expect(page.getByRole("status")).toHaveText("Encoded.");
		expect(decode(await exchange.inputValue()).blueprint?.label).toBe(label);
		await page.getByRole("tab", { name: "Tree" }).click();
		await expect(page.getByRole("tree", { name: "Blueprint JSON tree" })).toContainText(label);

		await page.getByRole("button", { name: "Clear string" }).click();
		await expect(exchange).toHaveValue("");
	});

	test("opens an entry's blueprint as JSON", async ({ page, seed }) => {
		const entry = await seed.entry({ title: `Json ${uniqueWord()}`, blueprint: BLUEPRINT });

		await page.goto(`/e/${entry.id}`);
		await page.getByRole("button", { name: "Open as JSON" }).click();
		await expect(page).toHaveURL(/\/json\?from=/);
		// The decoded JSON streams in after the status: read it once loaded.
		await ready(page);
		await expect(page.getByRole("status")).toHaveText("Decoded.");
		await expect(page.getByRole("textbox", { name: "Exchange string" })).toHaveValue(/^0e/);
		expect(
			JSON.parse(await page.getByRole("textbox", { name: "JSON", exact: true }).inputValue()),
		).toEqual(decode(BLUEPRINT));
	});

	test("opens a planner of a book as JSON", async ({ page, seed }) => {
		const entry = await seed.entry({
			title: `Json planners ${uniqueWord()}`,
			blueprint: DECONSTRUCTION_BOOK,
		});

		await page.goto(`/e/${entry.id}`);
		const cliffs = page.locator("details.planner").filter({ hasText: "Cliffs" }).first();
		await cliffs.locator(":scope > summary").click();
		await cliffs.getByText("Deconstruction planner string").click();
		await cliffs.getByRole("button", { name: "Open as JSON" }).click();
		await expect(page).toHaveURL(/\/json\?from=/);
		await ready(page);
		const decoded = JSON.parse(
			await page.getByRole("textbox", { name: "JSON", exact: true }).inputValue(),
		);
		expect(decoded.deconstruction_planner.label).toBe("Cliffs");
	});

	test("says when the blueprint to open is missing", async ({ page }) => {
		await page.goto("/json?from=aaaaaaaaaaaa");
		await expect(page.getByRole("status")).toHaveText(
			"That blueprint does not exist or is not visible to you.",
		);
	});

	test("explains JSON it cannot encode", async ({ page }) => {
		await page.goto("/json");
		await ready(page);
		const json = page.getByRole("textbox", { name: "JSON", exact: true });
		const encode = page.getByRole("button", { name: "Encode to string" });
		const status = page.getByRole("status");

		await json.fill("{not json");
		await encode.click();
		await expect(status).toHaveText(/^The JSON does not parse: /);

		// Valid JSON that is no blueprint: the API's reason.
		await json.fill('{"label": "no envelope"}');
		await encode.click();
		await expect(status).toHaveText(
			"That does not look like a Factorio blueprint string. Copy it again from the game.",
		);
	});
});
