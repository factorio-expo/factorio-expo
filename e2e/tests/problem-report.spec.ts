// The bug button: on every page, anyone sends a problem report with the
// page's context (on an entry page its entry, blueprint and states) and an
// optional contact address. Staff review them in the console.
import { uniqueWord } from "../support/api.ts";
import { expect, test } from "../support/fixtures.ts";
import { ready } from "../support/ui.ts";

test("a guest reports a problem with a preview, with the page's context", async ({
	page,
	seed,
}) => {
	const entry = await seed.entry({ title: `Broken ${uniqueWord()}` });
	await page.goto(`/e/${entry.id}`);
	await ready(page);

	await page.getByRole("button", { name: "Report a problem with this preview" }).click();
	const dialog = page.getByRole("dialog", { name: "Report a problem" });
	await expect(dialog).toBeVisible();
	await dialog.getByText("Sent with your report").click();
	await expect(dialog).toContainText(entry.id);
	await expect(dialog).toContainText("Window size");

	const sent = page.waitForRequest(
		(request) =>
			request.url().endsWith("/api/v1/problem-reports") && request.method() === "POST",
	);
	await dialog.getByLabel("What went wrong?").fill("The preview is cut off on the right.");
	await dialog.getByRole("button", { name: "Send report" }).click();
	const body = (await sent).postDataJSON() as Record<string, unknown>;
	expect(body).toMatchObject({
		body: "The preview is cut off on the right.",
		page: `/e/${entry.id}`,
		entry_id: entry.id,
	});
	expect(body.blueprint_id).toMatch(/^[0-9A-Za-z]{12}$/);
	expect(body).not.toHaveProperty("email");
	await expect(dialog).toContainText("on the admins' list");

	// Opened again, it starts over.
	await dialog.getByRole("button", { name: "Close" }).click();
	await page.getByRole("button", { name: "Report a problem with this preview" }).click();
	await expect(dialog.getByLabel("What went wrong?")).toHaveValue("");
});

test.describe("as a member", () => {
	test.use({ viewer: "member" });

	test("reports a problem from the footer with a contact address", async ({ page }) => {
		await page.goto("/about");
		await ready(page);
		await page
			.getByRole("contentinfo")
			.getByRole("button", { name: "Report a problem" })
			.click();
		const dialog = page.getByRole("dialog", { name: "Report a problem" });
		await dialog.getByLabel("What went wrong?").fill("The about page has a typo.");
		await dialog.getByLabel(/^\s*Email/).fill("member@e2e.test");
		await dialog.getByRole("button", { name: "Send report" }).click();
		await expect(dialog).toContainText("they will write to member@e2e.test");
	});

	test("the header's bug icon opens the same dialog", async ({ page }) => {
		await page.goto("/");
		await ready(page);
		await page.getByRole("banner").getByRole("button", { name: "Report a problem" }).click();
		await expect(page.getByRole("dialog", { name: "Report a problem" })).toBeVisible();
	});
});

test.describe("on a phone", () => {
	test.use({ viewport: { width: 390, height: 844 } });

	test("the side drawer has the bug button", async ({ page }) => {
		await page.goto("/");
		await ready(page);
		await expect(
			page.getByRole("banner").getByRole("button", { name: "Report a problem" }),
		).toBeHidden();
		await page.getByRole("button", { name: "Open menu" }).click();
		await page
			.getByRole("complementary")
			.getByRole("button", { name: "Report a problem" })
			.click();
		await expect(page.getByRole("dialog", { name: "Report a problem" })).toBeVisible();
	});
});
