// Steps several specs share, written the way a visitor would do them.
import { expect, type Locator, type Page } from "@playwright/test";

/**
 * Opens an exchange string panel (they start collapsed) by its heading, e.g.
 * "Blueprint" for "Blueprint string", and returns the string's textbox.
 */
export async function openExchangeString(page: Page, label: string): Promise<Locator> {
	await page
		.locator("[data-blueprint-export] summary")
		.filter({ hasText: new RegExp(`^\\s*${label} string`) })
		.first()
		.click();
	const textbox = page.getByRole("textbox", { name: `${label} exchange string` });
	await expect(textbox).toBeVisible();
	return textbox;
}

/**
 * A blueprint's card in the book tree, by name. Only the root book starts
 * open, so every closed sub-book is opened first, the way a visitor would.
 */
export async function bookLeaf(page: Page, name: string): Promise<Locator> {
	const tree = page.locator(".tree");
	await expect(tree).toBeVisible();
	const closed = tree.locator("details:not([open]):not(.planner) > summary");
	while ((await closed.count()) > 0) await closed.first().click();
	return tree.getByRole("link", { name });
}

/** Fills the upload form (no submit). */
export async function fillUploadForm(
	page: Page,
	fields: {
		blueprint: string;
		title: string;
		description?: string;
		tags?: string[];
		expires?: string;
		/** A Visibility option label; members only (guests get no such field). */
		visibility?: string;
		/** Ticks "Spoiler or NSFW". */
		spoiler?: boolean;
		/** The vanity alias; members only (guests get no such field). */
		alias?: string;
	},
): Promise<void> {
	await page.getByLabel("Blueprint exchange string").fill(fields.blueprint);
	await page.getByLabel("Entry title").fill(fields.title);
	if (fields.description) await page.getByLabel(/^\s*Entry description/).fill(fields.description);
	if (fields.tags?.length) {
		await page.getByText("Select tags...").click();
		for (const tag of fields.tags)
			await page.getByRole("checkbox", { name: tag, exact: true }).check();
		await page.keyboard.press("Escape");
	}
	if (fields.visibility) await page.getByLabel("Visibility").selectOption(fields.visibility);
	if (fields.expires) await page.getByLabel("Expires").selectOption(fields.expires);
	if (fields.spoiler) await page.getByRole("checkbox", { name: /Spoiler or NSFW/ }).check();
	if (fields.alias) await page.getByLabel(/^\s*Short link/).fill(fields.alias);
}

/** The entry page's details panel (uploader, dates, revision, actions). */
export const entryDetails = (page: Page) => page.getByRole("region", { name: "Entry details" });

/**
 * Waits until the current page has loaded. Script-driven buttons (inline edit,
 * revision picker, favorites, collections) ignore clicks that land before their
 * script runs, so call this after a click navigated and before using them.
 * (Form submit buttons stay disabled until then, which Playwright waits for.)
 */
export async function ready(page: Page): Promise<void> {
	await page.waitForLoadState("load");
}

/**
 * Holds back the page's scripts, as a slow connection would, until the
 * returned function is called. Navigate with `waitUntil: "commit"`: module
 * scripts hold up DOMContentLoaded and load.
 */
export async function holdScripts(page: Page): Promise<() => void> {
	let release!: () => void;
	const released = new Promise<void>((resolve) => (release = resolve));
	await page.route("**/assets/*.js", async (route) => {
		await released;
		// The page may have navigated away meanwhile.
		await route.continue().catch(() => undefined);
	});
	return release;
}

/** The form's submit button (the header has an "Upload" link too). */
export async function submitUpload(page: Page): Promise<void> {
	await page.getByRole("main").getByRole("button", { name: "Upload" }).click();
}

/** The revision picker button; its text names the revision on screen. */
export const revisionButton = (page: Page) =>
	page.getByRole("button", { name: "Revision history" });

/** Uploads through /upload and returns the new entry's id once its page shows. */
export async function uploadThroughUi(
	page: Page,
	fields: {
		blueprint: string;
		title: string;
		description?: string;
		tags?: string[];
		expires?: string;
		/** A Visibility option label; members only (guests get no such field). */
		visibility?: string;
		spoiler?: boolean;
		alias?: string;
	},
): Promise<string> {
	await page.goto("/upload");
	await fillUploadForm(page, fields);
	await submitUpload(page);
	await expect(page.getByRole("heading", { level: 1, name: fields.title })).toBeVisible();
	return entryIdFromUrl(page);
}

/** The quick preview panel on a preview's entry page. */
export const previewPanel = (page: Page) => page.getByRole("region", { name: "Quick preview" });

/** Expands the collapsed publish form of a publish panel (quick preview or anonymous entry). */
export async function openPublishForm(panel: Locator): Promise<void> {
	await panel.getByText("Publish to my account").click();
	await expect(panel.getByLabel("Entry title")).toBeVisible();
}

/** Creates a quick preview through /preview and returns its id once its page shows. */
export async function previewThroughUi(page: Page, blueprint: string): Promise<string> {
	await page.goto("/preview");
	await page.getByLabel("Blueprint exchange string").fill(blueprint);
	await page.getByRole("main").getByRole("button", { name: "Preview" }).click();
	await expect(previewPanel(page)).toBeVisible();
	return entryIdFromUrl(page);
}

export function entryIdFromUrl(page: Page): string {
	const match = new URL(page.url()).pathname.match(/^\/e\/([0-9A-Za-z]{12})/);
	if (!match) throw new Error(`not on an entry page: ${page.url()}`);
	return match[1];
}

/** Opens the revision history dialog and picks one revision by its link text. */
export async function pickRevision(page: Page, name: string | RegExp): Promise<void> {
	await ready(page);
	await revisionButton(page).click();
	const dialog = page.getByRole("dialog", { name: "Pick a revision" });
	await expect(dialog).toBeVisible();
	await dialog.getByRole("link", { name }).click();
}

/** Edits the entry title inline (pencil, field, Save) and waits for the reload. */
export async function editTitle(page: Page, title: string): Promise<void> {
	await ready(page);
	await page.getByRole("button", { name: "Edit details" }).click();
	await page.getByLabel("Entry title").fill(title);
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.getByRole("heading", { level: 1, name: title })).toBeVisible();
}

/** Edits (or adds) the entry description inline and waits for the reload. */
export async function editDescription(
	page: Page,
	description: string,
	shown: string,
): Promise<void> {
	await ready(page);
	await page.getByRole("button", { name: "Edit details" }).click();
	await page.getByLabel(/^\s*Entry description/).fill(description);
	await page.getByRole("button", { name: "Save" }).click();
	await expect(page.locator(".entry-description")).toContainText(shown);
}

/** Replaces the exchange string on the new-revision page (with an optional note) and submits it. */
export async function submitRevision(page: Page, blueprint: string, note?: string): Promise<void> {
	await expect(page.getByRole("heading", { level: 1, name: "Create a revision" })).toBeVisible();
	await page.getByLabel("Blueprint exchange string").fill(blueprint);
	if (note) await page.getByLabel(/^\s*What changed\?/).fill(note);
	await page.getByRole("button", { name: "Create revision" }).click();
	await expect(page).toHaveURL(/\/e\/[0-9A-Za-z]{12}(\?token=[^&]+)?$/);
}
