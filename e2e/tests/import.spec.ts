// The upload page's three tabs, "Paste string", "From file" and "Import from link": importing a
// blueprint from a link (the API fetches it, here from the stack's stand-in
// server, never the real sites, and inspects it), the consent line that gates
// it, the review step it shares with a pasted string, and the source token that
// proves an upload came from the link it names.
import { request, type APIRequestContext, type Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, relabel, SPACE_AGE_20 } from "../support/blueprints.ts";
import { expect, test, viewers } from "../support/fixtures.ts";
import { stubRequests, stubSite } from "../support/import-stub.ts";
import { apiURL, webURL } from "../support/stack.ts";
import { entryDetails, ready, submitUpload } from "../support/ui.ts";

const consent = "I made this blueprint or have the right to share it here";

/** A paste id no other test uses. */
const pasteId = () => `P${Math.random().toString(36).slice(2, 10)}`.slice(0, 8).padEnd(8, "x");

const pasteTab = (page: Page) => page.getByRole("tab", { name: "Paste string" });
const importTab = (page: Page) => page.getByRole("tab", { name: "Import from link" });
const linkField = (page: Page) => page.getByLabel("Link", { exact: true });
const fetchButton = (page: Page) => page.getByRole("button", { name: "Import", exact: true });
const uploadButton = (page: Page) =>
	page.getByRole("main").getByRole("button", { name: "Upload", exact: true });
const importSummary = (page: Page) => page.locator("[data-import-summary]");

/** Opens the upload page's import tab and fills the link. */
async function enterLink(page: Page, link: string) {
	await ready(page);
	await importTab(page).click();
	await linkField(page).fill(link);
}

/** Makes the stub serve `body` as a pastebin paste and returns the link to it. */
async function stubPaste(body: string): Promise<{ id: string; link: string }> {
	const id = pasteId();
	await stubSite(`/pastebin/raw/${id}`, { body });
	return { id, link: `https://pastebin.com/${id}` };
}

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("imports a pastebin link, shows the inspection and uploads it with its source", async ({
			page,
		}) => {
			const label = `Imported ${uniqueWord()}`;
			const { id, link } = await stubPaste(relabel(SPACE_AGE_20, label));
			await page.goto("/upload");
			await enterLink(page, link);
			await page.getByLabel(consent).check();
			// Nothing to upload before the fetch.
			await expect(uploadButton(page)).toBeDisabled();
			await fetchButton(page).click();

			// The same review step as a pasted string: summary, copy button, tag suggestions.
			await expect(page.getByText(/Imported\. Check the details below/)).toBeVisible();
			await expect(page.getByText("Imported from pastebin.com.")).toBeVisible();
			await expect(importSummary(page)).toContainText(label);
			await expect(importSummary(page)).toContainText(/entit/i);
			await expect(
				page.getByRole("list", { name: "Suggested tags:" }).getByRole("button"),
			).toHaveText(["Bot network"]);
			await expect(uploadButton(page)).toBeEnabled();
			// The paste tab's field is untouched.
			await expect(page.getByLabel("Blueprint exchange string")).toHaveValue("");

			await page.getByRole("button", { name: "Import details from blueprint" }).click();
			await expect(page.getByLabel("Entry title")).toHaveValue(label);
			const title = `Entry ${uniqueWord()}`;
			await page.getByLabel("Entry title").fill(title);
			await submitUpload(page);

			await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
			const source = entryDetails(page).getByRole("link", { name: "pastebin.com" });
			await expect(source).toHaveAttribute("href", `https://pastebin.com/${id}`);
			await expect(entryDetails(page).getByText("Imported from")).toBeVisible();
		});

		test("switching tabs keeps what each one holds and the button follows the active tab", async ({
			page,
		}) => {
			const typed = relabel(SPACE_AGE_20, `Typed ${uniqueWord()}`);
			const imported = relabel(SPACE_AGE_20, `Fetched ${uniqueWord()}`);
			const { link } = await stubPaste(imported);
			await page.goto("/upload");
			await ready(page);

			// The tabs are an ARIA tablist: the first is selected, the other panel is hidden.
			await expect(
				page.getByRole("tablist", { name: "Where the blueprint comes from" }),
			).toBeVisible();
			await expect(pasteTab(page)).toHaveAttribute("aria-selected", "true");
			await expect(importTab(page)).toHaveAttribute("aria-selected", "false");
			await expect(page.getByRole("tabpanel")).toHaveCount(1);
			await expect(uploadButton(page)).toBeDisabled();

			await page.getByLabel("Blueprint exchange string").fill(typed);
			await expect(uploadButton(page)).toBeEnabled();

			// Arrow keys move between tabs and show their panel.
			await pasteTab(page).focus();
			await page.keyboard.press("ArrowRight");
			await expect(page.getByRole("tab", { name: "From file" })).toBeFocused();
			await page.keyboard.press("ArrowRight");
			await expect(importTab(page)).toBeFocused();
			await expect(importTab(page)).toHaveAttribute("aria-selected", "true");
			await expect(pasteTab(page)).toHaveAttribute("aria-selected", "false");
			await expect(linkField(page)).toBeVisible();
			await expect(page.getByLabel("Blueprint exchange string")).toBeHidden();
			// Nothing imported yet: the typed string does not count here.
			await expect(uploadButton(page)).toBeDisabled();

			await linkField(page).fill(link);
			await page.getByLabel(consent).check();
			await fetchButton(page).click();
			await expect(page.getByText("Imported from pastebin.com.")).toBeVisible();
			await expect(uploadButton(page)).toBeEnabled();

			// Back: the typed string is still there, and is what the button would upload.
			await importTab(page).focus();
			await page.keyboard.press("Home");
			await expect(pasteTab(page)).toHaveAttribute("aria-selected", "true");
			await expect(page.getByLabel("Blueprint exchange string")).toHaveValue(typed);
			await expect(
				page.getByRole("list", { name: "Suggested tags:" }).getByRole("button"),
			).toHaveText(["Bot network"]);
			await expect(uploadButton(page)).toBeEnabled();
			// Emptying it turns the button off even though the other tab holds an import.
			await page.getByLabel("Blueprint exchange string").fill("");
			await expect(uploadButton(page)).toBeDisabled();
			await page.getByLabel("Blueprint exchange string").fill(typed);
			await expect(uploadButton(page)).toBeEnabled();

			// The link tab kept its link, consent and imported blueprint.
			await importTab(page).click();
			await expect(linkField(page)).toHaveValue(link);
			await expect(page.getByLabel(consent)).toBeChecked();
			await expect(page.getByText("Imported from pastebin.com.")).toBeVisible();
			await expect(importSummary(page)).toContainText("Fetched");

			// Uploading from the paste tab sends the typed string with no source.
			await pasteTab(page).click();
			const title = `Typed ${uniqueWord()}`;
			await page.getByLabel("Entry title").fill(title);
			await submitUpload(page);
			await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
			await expect(entryDetails(page).getByText("Imported from")).toBeHidden();
		});

		test("a text file picked from the computer is read in the browser like a pasted string", async ({
			page,
		}) => {
			const label = `File ${uniqueWord()}`;
			await page.goto("/upload");
			await ready(page);
			await page.getByRole("tab", { name: "From file" }).click();
			await expect(uploadButton(page)).toBeDisabled();

			await page.getByLabel("Text file with the exchange string").setInputFiles({
				name: "blueprint.txt",
				mimeType: "text/plain",
				buffer: Buffer.from(`${relabel(BLUEPRINT, label)}\n`),
			});
			await expect(page.locator("[data-file-summary]")).toContainText(label);
			await expect(uploadButton(page)).toBeEnabled();
			// The paste tab's field is untouched.
			await expect(page.getByLabel("Blueprint exchange string")).toHaveValue("");

			await page.getByLabel("Text file with the exchange string").setInputFiles({
				name: "empty.txt",
				mimeType: "text/plain",
				buffer: Buffer.from("  \n"),
			});
			await expect(page.getByText("empty.txt is empty.")).toBeVisible();
			await expect(uploadButton(page)).toBeDisabled();
		});

		test("an upload clears the form, so going back shows empty fields", async ({ page }) => {
			const title = `Cleared ${uniqueWord()}`;
			await page.goto("/upload");
			await ready(page);
			await page
				.getByLabel("Blueprint exchange string")
				.fill(relabel(BLUEPRINT, `Typed ${uniqueWord()}`));
			await page.getByLabel("Entry title").fill(title);
			await submitUpload(page);
			await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();

			await page.goBack();
			await ready(page);
			await expect(page.getByLabel("Blueprint exchange string")).toHaveValue("");
			await expect(page.getByLabel("Entry title")).toHaveValue("");
		});

		test("blocks the import and the upload until the consent line is ticked", async ({
			page,
		}) => {
			const { id, link } = await stubPaste(relabel(BLUEPRINT, uniqueWord()));
			await page.goto("/upload");
			await enterLink(page, link);
			await fetchButton(page).click();
			await expect(page.getByText(`Tick the box first: ${consent}.`)).toBeVisible();
			await expect(uploadButton(page)).toBeDisabled();
			// Nothing was fetched without the statement.
			expect(await stubRequests()).not.toContain(`/pastebin/raw/${id}`);

			// Ticked, imported, then unticked: the upload button goes off again.
			await page.getByLabel(consent).check();
			await fetchButton(page).click();
			await expect(page.getByText("Imported from pastebin.com.")).toBeVisible();
			await expect(uploadButton(page)).toBeEnabled();
			await page.getByLabel(consent).uncheck();
			await expect(
				page.getByText(`Tick the box to upload an imported blueprint: ${consent}.`),
			).toBeVisible();
			await expect(uploadButton(page)).toBeDisabled();
			await page.getByLabel(consent).check();
			await expect(uploadButton(page)).toBeEnabled();
			await page.getByLabel("Entry title").fill(`Entry ${uniqueWord()}`);
			await submitUpload(page);
			await expect(page).toHaveURL(/\/e\/[0-9A-Za-z]{12}/);
		});

		test("another link is another import", async ({ page }) => {
			const { link } = await stubPaste(relabel(BLUEPRINT, uniqueWord()));
			await page.goto("/upload");
			await enterLink(page, link);
			await page.getByLabel(consent).check();
			await fetchButton(page).click();
			await expect(page.getByText("Imported from pastebin.com.")).toBeVisible();
			await expect(uploadButton(page)).toBeEnabled();

			await linkField(page).fill(`${link}x`);
			await expect(page.getByText("Imported from pastebin.com.")).toBeHidden();
			await expect(importSummary(page)).toBeHidden();
			await expect(uploadButton(page)).toBeDisabled();
		});

		test("explains a link it cannot import and uploads nothing", async ({ page }) => {
			const missing = await stubPaste("unused");
			await stubSite(`/pastebin/raw/${missing.id}`, { status: 404, body: "not found" });
			const prose = await stubPaste("just some words, no blueprint");
			// Starts with the version marker, but is no blueprint: never handed back.
			const secret = await stubPaste("0 SECRET-internal-value: hunter2");
			await page.goto("/upload");
			await enterLink(page, "https://example.com/aBcD1234");
			await page.getByLabel(consent).check();

			await fetchButton(page).click();
			await expect(page.getByText("links from example.com are not supported")).toBeVisible();
			await linkField(page).fill(missing.link);
			await fetchButton(page).click();
			await expect(page.getByText(/nothing at that link/)).toBeVisible();
			await linkField(page).fill(prose.link);
			await fetchButton(page).click();
			await expect(
				page.getByText("no blueprint string was found at that link"),
			).toBeVisible();
			await linkField(page).fill(secret.link);
			await fetchButton(page).click();
			await expect(
				page.getByText("no blueprint string was found at that link"),
			).toBeVisible();
			await expect(page.getByText("hunter2")).toBeHidden();
			await linkField(page).fill("http://pastebin.com/aBcD1234");
			await fetchButton(page).click();
			await expect(page.getByText("not a valid https link")).toBeVisible();

			// Nothing imported, nothing to upload; the paste tab is untouched.
			await expect(uploadButton(page)).toBeDisabled();
			await expect(page.getByText("Imported from pastebin.com.")).toBeHidden();
			await pasteTab(page).click();
			await expect(page.getByLabel("Blueprint exchange string")).toHaveValue("");
		});

		test("a failed fetch drops an earlier import", async ({ page }) => {
			const good = await stubPaste(relabel(BLUEPRINT, uniqueWord()));
			const missing = pasteId();
			await stubSite(`/pastebin/raw/${missing}`, { status: 404, body: "not found" });
			await page.goto("/upload");
			await enterLink(page, good.link);
			await page.getByLabel(consent).check();
			await fetchButton(page).click();
			await expect(uploadButton(page)).toBeEnabled();

			await linkField(page).fill(`https://pastebin.com/${missing}`);
			await fetchButton(page).click();
			await expect(page.getByText(/nothing at that link/)).toBeVisible();
			await expect(uploadButton(page)).toBeDisabled();
		});
	});
}

test("offers the title and description a site suggests, filling them only on click", async ({
	page,
}) => {
	const label = `Print ${uniqueWord()}`;
	const key = `-N${Math.random().toString(36).slice(2, 12)}abcdefghij`;
	await stubSite(`/factorioprints/blueprints/${key}.json`, {
		contentType: "application/json",
		body: JSON.stringify({
			title: `Suggested ${label}`,
			descriptionMarkdown: "Suggested **text**",
			blueprintString: relabel(BLUEPRINT, label),
		}),
	});
	await page.goto("/upload");
	await enterLink(page, `https://factorioprints.com/view/${key}`);
	await page.getByLabel(consent).check();
	await fetchButton(page).click();
	await expect(page.getByText(/Imported\. Check the details below/)).toBeVisible();
	// Nothing is filled in on its own.
	await expect(page.getByLabel("Entry title")).toHaveValue("");
	await page.getByRole("button", { name: "Import details from website" }).click();
	await expect(page.getByLabel("Entry title")).toHaveValue(`Suggested ${label}`);
	await expect(page.getByLabel(/^\s*Entry description/)).toHaveValue("Suggested **text**");
	// Editable, not final.
	await page.getByLabel("Entry title").fill(`Mine ${label}`);
	await submitUpload(page);
	await expect(page.getByRole("heading", { level: 1, name: `Mine ${label}` })).toBeVisible();
	await expect(
		entryDetails(page).getByRole("link", { name: "factorioprints.com" }),
	).toBeVisible();
});

test.describe("on a phone", () => {
	test.use({ viewport: { width: 375, height: 812 } });

	test("both tabs fit the screen and the import works", async ({ page }) => {
		const { link } = await stubPaste(relabel(BLUEPRINT, `Phone ${uniqueWord()}`));
		const fits = () =>
			page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth);
		await page.goto("/upload");
		await ready(page);
		await expect(pasteTab(page)).toBeVisible();
		await expect(importTab(page)).toBeVisible();
		expect(await fits()).toBe(true);

		await importTab(page).click();
		await linkField(page).fill(link);
		await page.getByLabel(consent).check();
		await expect(fetchButton(page)).toBeVisible();
		expect(await fits()).toBe(true);
		await fetchButton(page).click();
		await expect(page.getByText("Imported from pastebin.com.")).toBeVisible();
		await expect(importSummary(page)).toBeVisible();
		expect(await fits()).toBe(true);
		await expect(uploadButton(page)).toBeEnabled();
	});
});

async function api(): Promise<APIRequestContext> {
	return request.newContext({ baseURL: apiURL, extraHTTPHeaders: { Origin: webURL } });
}
const code = async (response: { json(): Promise<unknown> }) =>
	((await response.json()) as { code: string }).code;

test.describe("the API", () => {
	test("refuses an import request without consent and any other host", async () => {
		const client = await api();
		try {
			const denied = await client.post("/api/v1/imports/resolve", {
				data: { link: "https://pastebin.com/aBcD1234", consent: false },
			});
			expect(denied.status()).toBe(422);
			expect(await code(denied)).toBe("import_consent_required");
			for (const link of [
				"https://pastebin.com.evil.test/aBcD1234",
				"https://evil.test/pastebin.com/aBcD1234",
				"https://user@pastebin.com/aBcD1234",
				"https://127.0.0.1/aBcD1234",
				"http://pastebin.com/aBcD1234",
			]) {
				const refused = await client.post("/api/v1/imports/resolve", {
					data: { link, consent: true },
				});
				expect(refused.status(), link).toBe(422);
			}
			// None of the refused links reached the stand-in server.
			expect((await stubRequests()).filter((path) => path.includes("aBcD1234"))).toEqual([]);
		} finally {
			await client.dispose();
		}
	});

	test("answers only a blueprint, with its inspection and a source token", async () => {
		const client = await api();
		try {
			const label = `Resolved ${uniqueWord()}`;
			const good = await stubPaste(relabel(BLUEPRINT, label));
			const resolved = await client.post("/api/v1/imports/resolve", {
				data: { link: good.link, consent: true },
			});
			expect(resolved.status()).toBe(200);
			const body = (await resolved.json()) as {
				blueprint: string;
				source_url: string;
				source_token: string;
				inspection: { kind: string; label: string; entity_count: number };
			};
			expect(body.source_url).toBe(good.link);
			expect(body.blueprint).toBe(relabel(BLUEPRINT, label));
			expect(body.source_token.length).toBeGreaterThan(20);
			expect(body.inspection.label).toBe(label);
			expect(body.inspection.entity_count).toBeGreaterThan(0);

			// Text that is no blueprint is not an answer, whatever it starts with.
			const text = await stubPaste("0 SECRET-internal-value: hunter2");
			const refused = await client.post("/api/v1/imports/resolve", {
				data: { link: text.link, consent: true },
			});
			expect(refused.status()).toBe(422);
			expect(await code(refused)).toBe("import_no_blueprint");
			expect(await refused.text()).not.toContain("hunter2");
		} finally {
			await client.dispose();
		}
	});

	test("an upload that names a source needs the token of that import", async () => {
		const client = await api();
		try {
			const label = `Proven ${uniqueWord()}`;
			const first = await stubPaste(relabel(BLUEPRINT, label));
			const second = await stubPaste(relabel(BLUEPRINT, `Other ${uniqueWord()}`));
			const resolve = async (link: string) =>
				(await (
					await client.post("/api/v1/imports/resolve", { data: { link, consent: true } })
				).json()) as { blueprint: string; source_url: string; source_token: string };
			const imported = await resolve(first.link);
			const other = await resolve(second.link);
			const upload = (data: Record<string, unknown>) =>
				client.post("/api/v1/entries", {
					data: { blueprint: imported.blueprint, ...data },
				});

			// The claim alone, or with a token that is not for this link and string.
			for (const [name, data] of Object.entries({
				"no token": { source_url: first.link, import_consent: true },
				"a made-up token": {
					source_url: first.link,
					import_consent: true,
					source_token: "A".repeat(55),
				},
				"another import's token": {
					source_url: first.link,
					import_consent: true,
					source_token: other.source_token,
				},
				"the token on another link": {
					source_url: second.link,
					import_consent: true,
					source_token: imported.source_token,
				},
				"the token on another string": {
					blueprint: relabel(BLUEPRINT, `Edited ${uniqueWord()}`),
					source_url: first.link,
					import_consent: true,
					source_token: imported.source_token,
				},
			})) {
				const refused = await upload(data);
				expect(refused.status(), name).toBe(422);
				expect(await code(refused), name).toBe("import_source_unverified");
			}
			// No consent: refused before the token matters.
			const unconsented = await upload({
				source_url: first.link,
				source_token: imported.source_token,
			});
			expect(await code(unconsented)).toBe("import_consent_required");

			// The real token, the real string and link, and the consent: stored with its source.
			const created = await upload({
				source_url: first.link,
				import_consent: true,
				source_token: imported.source_token,
			});
			expect(created.status()).toBe(201);
			expect(((await created.json()) as { source_url: string }).source_url).toBe(first.link);
		} finally {
			await client.dispose();
		}
	});
});
