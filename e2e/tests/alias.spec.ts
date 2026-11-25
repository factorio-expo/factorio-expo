// Vanity aliases: /@{username}/{slug} serves an entry the signed-in owner gave
// a short name, canonical at /e/{id}. Changing the alias makes the old link a
// 404; anonymous uploads have none.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { entryDetails, ready, uploadThroughUi } from "../support/ui.ts";

/** A valid alias nobody else uses. */
const aliasWord = () => `alias-${uniqueWord().toLowerCase()}`;

const canonical = (page: Page) => page.locator('head link[rel="canonical"]');
const shortLink = (page: Page) => page.getByLabel(/^\s*Short link/);

test("an anonymous upload has no short link field", async ({ page }) => {
	await page.goto("/upload");
	await expect(page.getByLabel("Entry title")).toBeVisible();
	await expect(shortLink(page)).toBeHidden();
});

test.describe("as member", () => {
	test.use({ viewer: "member" });

	test("sets an alias at upload and opens the entry on it, canonical at /e/{id}", async ({
		page,
		me,
	}) => {
		const alias = aliasWord();
		const title = `Aliased ${uniqueWord()}`;
		const id = await uploadThroughUi(page, {
			blueprint: BLUEPRINT,
			title,
			alias: ` ${alias.toUpperCase()} `,
		});

		// The entry page names its short link.
		const link = entryDetails(page).getByRole("link", { name: `/@${me.username}/${alias}` });
		await expect(link).toBeVisible();
		await link.click();
		await expect(page).toHaveURL(`/@${me.username}/${alias}`);
		await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
		await expect(canonical(page)).toHaveAttribute("href", new RegExp(`/e/${id}$`));
		// The username is matched whatever its case, the slug in lower case.
		const upper = await page.goto(`/@${me.username.toUpperCase()}/${alias.toUpperCase()}`);
		expect(upper?.status()).toBe(200);
		await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
	});

	test("changing the alias makes the old one a 404, clearing removes it", async ({
		page,
		seed,
		me,
	}) => {
		const first = aliasWord();
		const second = aliasWord();
		const entry = await seed.entry({ owner: me, alias: first });
		await page.goto(`/@${me.username}/${first}`);
		await ready(page);
		await page.getByRole("button", { name: "Edit details" }).click();
		await expect(shortLink(page)).toHaveValue(first);
		await shortLink(page).fill(second);
		await page.getByRole("button", { name: "Save" }).click();
		await expect(
			entryDetails(page).getByRole("link", { name: `/@${me.username}/${second}` }),
		).toBeVisible();

		const old = await page.goto(`/@${me.username}/${first}`);
		expect(old?.status()).toBe(404);
		const current = await page.goto(`/@${me.username}/${second}`);
		expect(current?.status()).toBe(200);
		await expect(canonical(page)).toHaveAttribute("href", new RegExp(`/e/${entry.id}$`));

		await ready(page);
		await page.getByRole("button", { name: "Edit details" }).click();
		await shortLink(page).fill("");
		await page.getByRole("button", { name: "Save" }).click();
		await expect(entryDetails(page).getByText("Short link")).toBeHidden();
		expect((await page.goto(`/@${me.username}/${second}`))?.status()).toBe(404);
		expect((await page.goto(`/e/${entry.id}`))?.status()).toBe(200);
	});

	test("refuses a taken, reserved or malformed alias with a reason", async ({
		page,
		seed,
		me,
	}) => {
		const taken = aliasWord();
		await seed.entry({ owner: me, alias: taken });
		const entry = await seed.entry({ owner: me });
		await page.goto(`/e/${entry.id}`);
		await ready(page);
		await page.getByRole("button", { name: "Edit details" }).click();
		const status = page.locator("[data-metadata-status]");

		await shortLink(page).fill(taken);
		await page.getByRole("button", { name: "Save" }).click();
		await expect(status).toContainText("already use that alias");
		await shortLink(page).fill("new");
		await page.getByRole("button", { name: "Save" }).click();
		await expect(status).toContainText("reserved");
		await shortLink(page).fill("under_score");
		await page.getByRole("button", { name: "Save" }).click();
		await expect(status).toContainText("lowercase letters, digits and hyphens");
		await shortLink(page).fill("AbCdEfGhIjKl");
		await page.getByRole("button", { name: "Save" }).click();
		await expect(status).toContainText("entry id");
	});

	test("a private entry's alias is for its owner only, and it belongs to its owner's name", async ({
		page,
		seed,
		me,
		browser,
	}) => {
		const alias = aliasWord();
		await seed.entry({ owner: me, alias, visibility: "private" });
		expect((await page.goto(`/@${me.username}/${alias}`))?.status()).toBe(200);

		const visitor = await browser.newPage({ baseURL: new URL(page.url()).origin });
		expect((await visitor.goto(`/@${me.username}/${alias}`))?.status()).toBe(404);
		await visitor.close();

		const other = await seed.account();
		const publicAlias = aliasWord();
		await seed.entry({ owner: me, alias: publicAlias });
		expect((await page.goto(`/@${other.username}/${publicAlias}`))?.status()).toBe(404);
		expect((await page.goto(`/@${me.username}/${publicAlias}`))?.status()).toBe(200);
	});

	test("there is no /bp/{id} redirect", async ({ page, seed, me }) => {
		const entry = await seed.entry({ owner: me, alias: aliasWord() });
		expect((await page.goto(`/bp/${entry.id}`))?.status()).toBe(404);
	});
});
