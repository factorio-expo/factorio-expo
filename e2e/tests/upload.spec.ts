import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BOOK, PLATFORM, relabel } from "../support/blueprints.ts";
import { expect, header, test } from "../support/fixtures.ts";
import { apiURL } from "../support/stack.ts";
import {
	entryDetails,
	entryIdFromUrl,
	fillUploadForm,
	ready,
	submitUpload,
	uploadThroughUi,
} from "../support/ui.ts";

test.describe("as guest", () => {
	test("uploads a blueprint and keeps an edit link", async ({ page }) => {
		const title = `Guest upload ${uniqueWord()}`;
		await page.goto("/");
		await header(page).getByRole("button", { name: "Upload" }).click();
		await expect(
			page.getByRole("heading", { level: 1, name: "Upload a blueprint" }),
		).toBeVisible();
		// An anonymous entry is always public: there is nothing to choose.
		await expect(page.getByLabel("Visibility")).toBeHidden();

		await fillUploadForm(page, {
			blueprint: BLUEPRINT,
			title,
			description: "Uploaded *without* an account.",
			tags: ["Smelting", "Compact"],
		});
		await submitUpload(page);

		// The entry opens on its clean, shareable link; the edit link is shown once.
		await expect(page).toHaveURL(/\/e\/[0-9A-Za-z]{12}$/);
		await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
		await expect(page.locator(".entry-description em")).toHaveText("without");
		await expect(
			page.getByRole("list", { name: "Entry tags" }).getByRole("listitem"),
		).toHaveCount(2);
		await expect(page.getByTitle("Uploader")).toHaveText("Anonymous");
		// Never expires unless asked to.
		await expect(entryDetails(page)).not.toContainText("Expires");
		await expect(page.getByText("Save your edit link. It is shown only once.")).toBeVisible();
		const editLink = await page.getByLabel("Edit link").inputValue();
		expect(editLink).toContain(`/e/${entryIdFromUrl(page)}?token=`);
		await expect(page.getByRole("button", { name: "Edit details" })).toBeHidden();

		await page.reload();
		await expect(page.getByText("Save your edit link. It is shown only once.")).toBeHidden();

		// The edit link is the only key to the entry: it shows the edit controls.
		await page.goto(editLink);
		await expect(page.getByRole("button", { name: "Edit details" })).toBeVisible();
		await expect(page.getByRole("button", { name: "New revision" })).toBeVisible();
	});

	test("uploads a book", async ({ page }) => {
		const title = `Guest book ${uniqueWord()}`;
		await uploadThroughUi(page, { blueprint: BOOK, title });
		await expect(page.locator(".tree").getByText("Fluids")).toBeVisible();
	});

	test("uploads an entry that expires", async ({ page }) => {
		const title = `Expiring ${uniqueWord()}`;
		await uploadThroughUi(page, { blueprint: BLUEPRINT, title, expires: "After 7 days" });
		// Local time and the time left, once the page script upgraded the UTC text.
		await expect(entryDetails(page)).toContainText(
			/Expires\s*\d{4}-\d{2}-\d{2} \d{2}:\d{2} \(in 6 days, 23 hr\)/,
		);
	});

	test("summarizes a pasted string and copies its label", async ({ page }) => {
		const label = `Pasted ${uniqueWord()}`;
		await page.goto("/upload");
		await ready(page);
		const blueprint = page.getByLabel("Blueprint exchange string");
		const summary = page.locator("[data-blueprint-summary]");
		const copy = page.getByRole("button", {
			name: "Import details from blueprint",
		});
		await expect(copy).toBeHidden();

		await blueprint.fill(relabel(BOOK, label));
		await blueprint.blur();
		await expect(summary).toHaveText(new RegExp(`^Book "${label}" · 10 blueprints · `));
		await page.getByLabel("Entry title").fill("Typed first");
		await copy.click();
		await expect(page.getByLabel("Entry title")).toHaveValue(label);

		// A broken string is explained, and nothing can be uploaded until it is fixed.
		const upload = page.getByRole("main").getByRole("button", { name: "Upload", exact: true });
		await expect(upload).toBeEnabled();
		await blueprint.fill("0this-is-not-a-blueprint");
		await blueprint.blur();
		await expect(summary).toHaveText(
			"That does not look like a Factorio blueprint string. Copy it again from the game.",
		);
		await expect(copy).toBeHidden();
		await expect(upload).toBeDisabled();
		await blueprint.fill(relabel(BOOK, label));
		await expect(upload).toBeEnabled();
	});

	test("lists suggested tags to add with a click", async ({ page }) => {
		await page.goto("/upload");
		await ready(page);
		const blueprint = page.getByLabel("Blueprint exchange string");
		const suggestions = page.getByRole("list", { name: "Suggested tags:" });
		// By value: the boxes sit in the closed picker, out of the accessibility tree.
		const tag = (slug: string) => page.locator(`input[name="tags"][value="${slug}"]`);

		// The book's cells use requester and provider chests. Nothing is ticked unasked.
		await blueprint.fill(BOOK);
		await blueprint.blur();
		await expect(suggestions.getByRole("button")).toHaveText(["Bot network"]);
		await expect(tag("bot-network")).not.toBeChecked();
		await suggestions.getByRole("button", { name: "Bot network" }).click();
		await expect(tag("bot-network")).toBeChecked();
		await expect(suggestions).toBeHidden();

		// A new string lists its own suggestions; "Add all" ticks every one. The
		// platform fixture is swapped for other ships over time, so its expected
		// tags come from the API's inspection, not a fixed list.
		const inspection = await page.request.post(`${apiURL}/api/v1/blueprints/inspect`, {
			data: { blueprint: PLATFORM },
		});
		expect(inspection.ok()).toBe(true);
		const { suggested_tags: expected } = (await inspection.json()) as {
			suggested_tags: string[];
		};
		expect(expected.length).toBeGreaterThan(1);
		await blueprint.fill(PLATFORM);
		await blueprint.blur();
		await expect(suggestions.getByRole("button")).toHaveCount(expected.length);
		await page.getByRole("link", { name: "Add all" }).click();
		await expect(suggestions).toBeHidden();
		for (const slug of expected) await expect(tag(slug)).toBeChecked();
		await expect(page.getByText("Add all")).toBeHidden();
	});

	test("explains a broken exchange string and keeps the upload button off", async ({ page }) => {
		await page.goto("/upload");
		await fillUploadForm(page, {
			blueprint: "0this-is-not-a-blueprint",
			title: `Broken ${uniqueWord()}`,
		});
		await expect(page.locator("[data-blueprint-summary]")).toHaveText(
			"That does not look like a Factorio blueprint string. Copy it again from the game.",
		);
		await expect(
			page.getByRole("main").getByRole("button", { name: "Upload", exact: true }),
		).toBeDisabled();
		await expect(page).toHaveURL("/upload");
	});
});

test.describe("as member", () => {
	test.use({ viewer: "member" });

	test("uploads a blueprint owned by the account", async ({ page, me }) => {
		const title = `Member upload ${uniqueWord()}`;
		await page.goto("/upload");
		await fillUploadForm(page, { blueprint: BLUEPRINT, title, tags: ["Steam power"] });
		await submitUpload(page);

		await expect(page).toHaveURL(/\/e\/[0-9A-Za-z]{12}$/);
		await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
		await expect(page.getByTitle("Uploader")).toHaveText(me.username);
		await expect(page.getByRole("button", { name: "Edit details" })).toBeVisible();
		await expect(entryDetails(page)).not.toContainText("Visibility");

		await page.goto(`/u/${me.username}`);
		await expect(page.getByRole("heading", { name: title })).toBeVisible();
	});

	test("defaults the visibility to public", async ({ page }) => {
		await page.goto("/upload");
		await expect(page.getByLabel("Visibility")).toHaveValue("public");
	});

	test("an unlisted upload opens for anyone with the link but is never listed", async ({
		page,
		seed,
		me,
	}) => {
		const word = uniqueWord();
		const title = `Unlisted ${word}`;
		const id = await uploadThroughUi(page, {
			blueprint: BLUEPRINT,
			title,
			visibility: "Unlisted (anyone with the link)",
		});
		await expect(entryDetails(page)).toContainText(
			"Unlisted: only people with the link can see it",
		);
		await expect(page.getByRole("button", { name: "Edit details" })).toBeVisible();

		// Not on the public profile, and not in search (a public twin proves the index caught up).
		const listed = await seed.entry({ title: `Listed ${word}`, owner: me });
		await page.goto(`/u/${me.username}`);
		await expect(page.getByRole("heading", { level: 2, name: listed.title })).toBeVisible();
		await expect(page.getByRole("heading", { level: 2, name: title })).toBeHidden();
		await seed.searchable(listed, word);
		await page.goto(`/browse?q=${encodeURIComponent(word)}`);
		await expect(page.getByRole("heading", { level: 2, name: listed.title })).toBeVisible();
		await expect(page.getByRole("heading", { level: 2, name: title })).toBeHidden();

		// Signed out, the link still opens it, without edit or delete controls.
		await page.context().clearCookies();
		const response = await page.goto(`/e/${id}`);
		expect(response?.headers()["x-robots-tag"]).toBe("noindex, nofollow");
		await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
		await expect(page.getByRole("button", { name: "Edit details" })).toBeHidden();
		await expect(page.getByRole("button", { name: "New revision" })).toBeHidden();
		await expect(page.getByRole("button", { name: "Delete entry" })).toBeHidden();
	});

	test("a private upload is hidden from everyone else", async ({ page }) => {
		const title = `Private ${uniqueWord()}`;
		const id = await uploadThroughUi(page, {
			blueprint: BLUEPRINT,
			title,
			visibility: "Private (only you)",
		});
		await expect(entryDetails(page)).toContainText("Private: only you can see it");

		await page.context().clearCookies();
		await page.goto(`/e/${id}`);
		await expect(page.getByRole("heading", { level: 1, name: "Not found" })).toBeVisible();
	});

	test("an expiring upload shows its expiry on its cards", async ({ page, me }) => {
		const title = `Member expiring ${uniqueWord()}`;
		await uploadThroughUi(page, { blueprint: BLUEPRINT, title, expires: "After 30 days" });
		await expect(entryDetails(page)).toContainText(/\(in 29 days, 23 hr\)/);

		await page.goto(`/u/${me.username}`);
		await ready(page);
		const card = page.getByRole("article").filter({ hasText: title });
		await expect(card).toContainText(/Expires\s+\d{4}-\d{2}-\d{2}/);
	});

	test("uploads a book owned by the account", async ({ page, me }) => {
		const title = `Member book ${uniqueWord()}`;
		await uploadThroughUi(page, { blueprint: BOOK, title });
		await expect(page).not.toHaveURL(/token=/);
		await expect(page.getByTitle("Uploader")).toHaveText(me.username);
		await expect(page.locator(".tree").getByText("Assemblers")).toBeVisible();
	});
});
