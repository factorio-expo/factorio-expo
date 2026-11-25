import type { Page } from "@playwright/test";
import { expect, header, test } from "../support/fixtures.ts";

const pageBackground = (page: Page) =>
	page.evaluate(() => getComputedStyle(document.documentElement).backgroundColor);

test("switches between light and dark mode and remembers the choice", async ({ page }) => {
	await page.goto("/");
	const root = page.locator("html");
	await expect(root).toHaveAttribute("data-theme", "light");
	const light = await pageBackground(page);

	await page.getByRole("button", { name: "Switch to dark theme" }).click();
	await expect(root).toHaveAttribute("data-theme", "dark");
	expect(await pageBackground(page)).not.toBe(light);

	// The choice follows the visitor to other pages and survives a reload.
	await header(page).getByRole("button", { name: "Upload" }).click();
	await expect(page.getByRole("heading", { name: "Upload a blueprint" })).toBeVisible();
	await expect(root).toHaveAttribute("data-theme", "dark");

	await page.getByRole("button", { name: "Switch to light theme" }).click();
	await expect(root).toHaveAttribute("data-theme", "light");
	await page.reload();
	await expect(root).toHaveAttribute("data-theme", "light");
	expect(await pageBackground(page)).toBe(light);
});

test.describe("with a dark system preference", () => {
	test.use({ colorScheme: "dark" });

	test("starts dark until the visitor switches", async ({ page }) => {
		await page.goto("/");
		await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
		await page.getByRole("button", { name: "Switch to light theme" }).click();
		await page.reload();
		await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
	});
});

test.describe("signed in", () => {
	test.use({ viewer: "member" });

	test("switches theme on account pages too", async ({ page }) => {
		await page.goto("/account");
		await page.getByRole("button", { name: "Switch to dark theme" }).click();
		await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
		await page
			.getByRole("navigation", { name: "Sidebar" })
			.getByRole("link", { name: "Favorites" })
			.click();
		await expect(page.getByRole("heading", { level: 1, name: "Favorites" })).toBeVisible();
		await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
	});
});
