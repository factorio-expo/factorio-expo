// Test fixtures. Every test arranges its own data (fresh accounts, uniquely
// named entries), so tests run in parallel and against a reused stack.
import { test as base, expect, type Page } from "@playwright/test";
import { Seeder, signIn, type Account } from "./api.ts";

export { expect };
export type { Account };

/** Who is looking: "member" signs the page's browser context in as a fresh account. */
export type Viewer = "guest" | "member";
export const viewers: Viewer[] = ["guest", "member"];

type Options = { viewer: Viewer };
type Fixtures = {
	seed: Seeder;
	/** The signed-in account for "member" viewers, undefined for guests. */
	account: Account | undefined;
	/** The signed-in account; fails the test when the viewer is a guest. */
	me: Account;
};

export const test = base.extend<Options & Fixtures>({
	viewer: ["guest", { option: true }],
	seed: async ({ browser }, use) => {
		const seeder = new Seeder(browser);
		await use(seeder);
		await seeder.dispose();
	},
	account: [
		async ({ viewer, seed, page }, use) => {
			if (viewer === "guest") {
				await use(undefined);
				return;
			}
			const account = await seed.account();
			await signIn(page, account);
			await use(account);
		},
		{ auto: true },
	],
	me: async ({ account }, use) => {
		if (!account) throw new Error('this test needs test.use({ viewer: "member" })');
		await use(account);
	},
});

/** The sidebar navigation. */
export const sidebar = (page: Page) => page.getByRole("navigation", { name: "Sidebar" });

/** The site header's actions (its links are styled, and exposed, as buttons). */
export const header = (page: Page) => page.getByRole("navigation", { name: "Main navigation" });

/** Header and sidebar state a signed-in visitor sees: log-out button, own profile link. */
export async function expectSignedIn(page: Page, username: string): Promise<void> {
	await expect(header(page).getByRole("button", { name: "Log out" })).toBeVisible();
	await expect(header(page).getByRole("button", { name: "Log in" })).toBeHidden();
	await expect(sidebar(page).getByRole("link", { name: "Public profile" })).toHaveAttribute(
		"href",
		`/u/${username}`,
	);
}

export async function expectSignedOut(page: Page): Promise<void> {
	await expect(header(page).getByRole("button", { name: "Log in" })).toBeVisible();
	await expect(header(page).getByRole("button", { name: "Log out" })).toBeHidden();
}
