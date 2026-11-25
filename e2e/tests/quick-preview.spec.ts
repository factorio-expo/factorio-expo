// Quick previews (docs/architecture.md "Quick preview"): one blueprint,
// rendered and analysed without publishing, unlisted, read-only, gone after
// its expiry or when it fails. The e2e stack has no Factorio, so a preview
// never finishes here: publishing a finished one is covered by the API tests;
// these cover the form, the pages and who may publish. The failure path pauses
// the shared render worker, so it lives with the other such test in
// live-progress.spec.ts.
import type { Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BOOK, relabel } from "../support/blueprints.ts";
import { expect, sidebar, test } from "../support/fixtures.ts";
import { signUpWithPasskey, useVirtualAuthenticator } from "../support/passkeys.ts";
import { openPublishForm, previewPanel as panel, previewThroughUi, ready } from "../support/ui.ts";

const banner = (page: Page) => page.getByText("Save your edit link. It is shown only once.");

test.describe("as guest", () => {
	test("previews a blueprint on an unlisted, read-only page", async ({ page }) => {
		await page.goto("/");
		await sidebar(page).getByRole("link", { name: "Preview" }).click();
		await expect(page.getByRole("heading", { level: 1, name: "Quick preview" })).toBeVisible();

		const id = await previewThroughUi(page, relabel(BLUEPRINT, `Quick ${uniqueWord()}`));
		await expect(page).toHaveURL(new RegExp(`/e/${id}$`));
		await expect(panel(page)).toContainText("Unlisted; deleted");
		await expect(page.getByText("Preview is rendering...")).toBeVisible();
		// Read-only: no edit, revision, delete, favorite or collection controls.
		for (const name of ["Edit details", "New revision", "Delete entry", "Add to favorites"]) {
			await expect(page.getByRole("button", { name })).toBeHidden();
		}

		// The edit link is shown once; it is what lets the creator publish.
		await expect(banner(page)).toBeVisible();
		const editLink = await page.getByLabel("Edit link").inputValue();
		expect(editLink).toContain(`/e/${id}?token=`);
		await page.reload();
		await expect(banner(page)).toBeHidden();
		await expect(panel(page)).toContainText("Only its creator can publish it.");

		await page.goto(editLink);
		await expect(panel(page).getByRole("link", { name: "Sign in to publish" })).toBeVisible();
	});

	test("signing in from the edit link offers publishing", async ({ page }) => {
		await useVirtualAuthenticator(page);
		await previewThroughUi(page, relabel(BLUEPRINT, `Claim ${uniqueWord()}`));
		await page.getByRole("status").getByRole("link", { name: "Sign in to publish" }).click();
		await ready(page);
		await signUpWithPasskey(page);

		await expect(page).toHaveURL(/\/e\/[0-9A-Za-z]{12}\?token=/);
		// The form starts collapsed; it warns that the edit link stops working.
		await expect(panel(page).getByLabel("Entry title")).toBeHidden();
		await openPublishForm(panel(page));
		await expect(panel(page)).toContainText("the edit link stops working for good");
		await expect(panel(page).getByRole("button", { name: "Publish" })).toBeDisabled();
	});

	test("refuses a book", async ({ page }) => {
		await page.goto("/preview");
		await page.getByLabel("Blueprint exchange string").fill(BOOK);
		await page.getByRole("main").getByRole("button", { name: "Preview" }).click();
		await expect(page.locator("[data-preview-status]")).toHaveText(
			"a quick preview takes a single blueprint, not a book",
		);
		await expect(page).toHaveURL("/preview");
	});
});

test.describe("as member", () => {
	test.use({ viewer: "member" });

	test("owns the preview; publishing waits for the render; it stays unlisted", async ({
		page,
		me,
	}) => {
		const label = `Mine ${uniqueWord()}`;
		await previewThroughUi(page, relabel(BLUEPRINT, label));
		await expect(banner(page)).toBeHidden();
		await expect(page.getByRole("button", { name: "Add to favorites" })).toBeHidden();
		await expect(page.getByRole("button", { name: "Add to collection" })).toBeHidden();
		await openPublishForm(panel(page));
		await expect(panel(page).getByLabel("Entry title")).toHaveValue(label);
		await expect(panel(page).getByRole("button", { name: "Publish" })).toBeDisabled();
		await expect(panel(page)).toContainText(
			"Publishing is available once the preview and analysis have finished.",
		);

		await page.goto("/account/entries");
		await ready(page);
		await expect(page.getByText(label)).toBeHidden();
		await page.goto(`/u/${me.username}`);
		await expect(page.getByText(label)).toBeHidden();
	});
});
