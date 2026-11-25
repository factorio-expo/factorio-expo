// Entry and leaf pages follow preview and analysis progress live over the
// API's event stream (docs/api.md "Entry progress events"). The e2e stack has
// no Factorio, so phases only settle by failing; previews otherwise stay
// pending, which keeps the stream open for as long as a test needs.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, oversized, relabel } from "../support/blueprints.ts";
import { expect, test, viewers } from "../support/fixtures.ts";
import { pauseRenderWorker } from "../support/stack.ts";
import { previewPanel, previewThroughUi } from "../support/ui.ts";

/** Resolves with the page's event-stream response once it is open. */
const eventStream = (page: Page, entryId: string) =>
	page.waitForResponse((response) =>
		response.url().endsWith(`/api/v1/entries/${entryId}/events`),
	);

for (const viewer of viewers) {
	test.describe(`as ${viewer}`, () => {
		test.use({ viewer });

		test("a pending blueprint follows its progress over one event stream", async ({
			page,
			seed,
		}) => {
			const entry = await seed.entry({
				blueprint: relabel(BLUEPRINT, `Live ${uniqueWord()}`),
			});

			const stream = eventStream(page, entry.id);
			await page.goto(`/e/${entry.id}`);
			await expect(page.getByText("Preview is rendering...")).toBeVisible();
			const response = await stream;
			expect(response.status()).toBe(200);
			expect(response.headers()["content-type"]).toBe("text/event-stream");
		});

		test("an open leaf page says when its entry is deleted", async ({ page, seed }) => {
			const entry = await seed.entry({
				blueprint: relabel(BLUEPRINT, `Doomed ${uniqueWord()}`),
			});

			const stream = eventStream(page, entry.id);
			await page.goto(`/e/${entry.id}`);
			await stream;
			await seed.deleteEntry(entry);

			await expect(page.getByRole("alert")).toHaveText(
				"This entry has been deleted or is no longer visible.",
			);
		});
	});
}

test.describe("a blueprint the renderer refuses", () => {
	// Pauses the shared render worker; one at a time.
	test.describe.configure({ mode: "serial" });

	test("shows the failure live, with its reason, without a reload", async ({ page, seed }) => {
		test.setTimeout(120_000);
		const resume = pauseRenderWorker();
		try {
			const entry = await seed.entry({
				blueprint: oversized(relabel(BLUEPRINT, `Huge ${uniqueWord()}`)),
			});
			const stream = eventStream(page, entry.id);
			await page.goto(`/e/${entry.id}`);
			await expect(page.getByText("Preview is rendering...")).toBeVisible();
			await stream;
			// Anything typed on the page survives the live update.
			await page.evaluate(() => {
				(window as { stillHere?: boolean }).stillHere = true;
			});
			resume();

			const reason = "The blueprint is too large to render.";
			await expect(page.getByText(`Preview could not be generated. ${reason}`)).toBeVisible({
				timeout: 60_000,
			});
			await expect(page.getByText(`Production analysis failed. ${reason}`)).toBeVisible();
			// Each failure note links the report dialog.
			await expect(page.getByText("if this looks wrong.")).toHaveCount(2);
			expect(await page.evaluate(() => (window as { stillHere?: boolean }).stillHere)).toBe(
				true,
			);

			// A fresh load renders the same settled state.
			await page.reload();
			await expect(page.getByText(`Preview could not be generated. ${reason}`)).toBeVisible();
		} finally {
			resume();
		}
	});

	test("a refused quick preview says why and is deleted", async ({ page }) => {
		test.setTimeout(120_000);
		const resume = pauseRenderWorker();
		try {
			const id = await previewThroughUi(
				page,
				oversized(relabel(BLUEPRINT, `Huge ${uniqueWord()}`)),
			);
			resume();
			await expect(previewPanel(page).getByRole("alert")).toHaveText(
				"The preview failed: The blueprint is too large to render. Nothing was kept. If this looks wrong, use Report a problem at the bottom of the page.",
				{ timeout: 60_000 },
			);
			const response = await page.goto(`/e/${id}`);
			expect(response?.status()).toBe(404);
		} finally {
			resume();
		}
	});

	// Result reuse (docs/architecture.md#result-reuse): a string that failed
	// for good is not sent to the renderer again.
	test("a refused blueprint uploaded again fails at once, and is no quick preview", async ({
		page,
		seed,
	}) => {
		test.setTimeout(120_000);
		const blueprint = oversized(relabel(BLUEPRINT, `Huge again ${uniqueWord()}`));
		const reason = "The blueprint is too large to render.";
		const first = await seed.entry({ blueprint });
		await page.goto(`/e/${first.id}`);
		await expect(page.getByText(`Preview could not be generated. ${reason}`)).toBeVisible({
			timeout: 60_000,
		});

		const again = await seed.entry({ blueprint });
		await page.goto(`/e/${again.id}`);
		await expect(page.getByText(`Preview could not be generated. ${reason}`)).toBeVisible();
		await expect(page.getByText(`Production analysis failed. ${reason}`)).toBeVisible();
		await expect(page.getByText("Preview is rendering...")).toHaveCount(0);

		await page.goto("/preview");
		await page.getByLabel("Blueprint exchange string").fill(blueprint);
		await page.getByRole("main").getByRole("button", { name: "Preview" }).click();
		await expect(
			page.getByText(`already failed to render, so it was not queued again: ${reason}`),
		).toBeVisible();
		await expect(page).toHaveURL(/\/preview$/);
	});
});
