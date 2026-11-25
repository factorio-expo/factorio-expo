// Signing in without passwords: passkeys through Chromium's virtual
// authenticator (support/passkeys.ts). Provider sign-ins (GitHub, Discord,
// Steam, Google) need real provider apps and are covered by the API's Go
// tests; here only their absence, the username step's empty state and the
// login page dropping an abandoned sign-up.
import { uniqueWord } from "../support/api.ts";
import { expect, expectSignedIn, expectSignedOut, header, test } from "../support/fixtures.ts";
import {
	createAccountWithPasskey,
	signUpWithPasskey,
	useVirtualAuthenticator,
} from "../support/passkeys.ts";
import { webURL } from "../support/stack.ts";
import { holdScripts, ready } from "../support/ui.ts";

const main = (page: import("@playwright/test").Page) => page.getByRole("main");

test.describe("create an account", () => {
	test("with a passkey, and the visitor is signed in", async ({ page }) => {
		await useVirtualAuthenticator(page);
		await page.goto("/");
		await header(page).getByRole("button", { name: "Log in" }).click();
		await expect(page.getByRole("heading", { level: 1, name: "Log in" })).toBeVisible();
		await ready(page);

		const { username } = await signUpWithPasskey(page);
		await expect(page).toHaveURL("/");
		await expectSignedIn(page, username);
	});

	test("explains why an account cannot be created", async ({ page, seed }) => {
		const taken = await seed.account();
		await useVirtualAuthenticator(page);
		await page.goto("/login");
		await ready(page);

		await createAccountWithPasskey(page, "no");
		await expect(main(page).getByRole("alert")).toHaveText(
			"Choose a username of 3-32 letters, digits, _ or -.",
		);
		await createAccountWithPasskey(page, taken.username.toUpperCase());
		await expect(main(page).getByRole("alert")).toHaveText(
			"That username is not available. Choose another one.",
		);
		// Reserved names read the same, whatever the case, '_' or '-'.
		await createAccountWithPasskey(page, "Ad_min");
		await expect(main(page).getByRole("alert")).toHaveText(
			"That username is not available. Choose another one.",
		);
		await expect(page).toHaveURL(/\/login$/);
		await expectSignedOut(page);
	});
});

test.describe("log in", () => {
	test("with the passkey made at sign-up, no username asked", async ({ page }) => {
		await useVirtualAuthenticator(page);
		await page.goto("/login");
		await ready(page);
		const { username } = await signUpWithPasskey(page);
		await expectSignedIn(page, username);
		await header(page).getByRole("button", { name: "Log out" }).click();
		await expect(page).toHaveURL("/");
		await expectSignedOut(page);

		await header(page).getByRole("button", { name: "Log in" }).click();
		await ready(page);
		await main(page).getByRole("button", { name: "Log in with a passkey" }).click();
		await expect(page).toHaveURL("/");
		await expectSignedIn(page, username);
	});

	test("returns to the account page that asked for it", async ({ page }) => {
		await useVirtualAuthenticator(page);
		await page.goto("/account/favorites");
		await expect(page).toHaveURL(/\/login\?next=/);
		await ready(page);

		await signUpWithPasskey(page);
		await expect(page).toHaveURL("/account/favorites");
		await expect(page.getByRole("heading", { level: 1, name: "Favorites" })).toBeVisible();
	});

	test("shows no provider buttons when none is configured", async ({ page }) => {
		await page.goto("/login");
		await expect(main(page).getByRole("button", { name: /^Continue with/ })).toHaveCount(0);
		await expect(
			main(page).getByRole("button", { name: "Log in with a passkey" }),
		).toBeVisible();
	});

	test("explains a provider sign-in that came back with an error", async ({ page }) => {
		await page.goto("/login?error=cancelled");
		await expect(main(page).getByRole("alert").first()).toHaveText("Sign-in was cancelled.");
	});

	test("the username step without a provider sign-in sends the visitor to log in", async ({
		page,
	}) => {
		await page.goto("/signup");
		await expect(page.getByText("No sign-up is waiting here")).toBeVisible();
		await page.getByRole("link", { name: "Log in", exact: true }).last().click();
		await expect(page).toHaveURL("/login");
	});

	test("opening the login page drops an abandoned provider sign-up", async ({ page }) => {
		// A provider sign-in that matched no account left its pending sign-up.
		await page.context().addCookies([
			{
				name: "__Host-expo_signup",
				value: "abandoned",
				domain: new URL(webURL).hostname,
				path: "/",
				secure: true,
				httpOnly: true,
				sameSite: "Lax",
			},
		]);
		await page.goto("/login");
		const cookies = await page.context().cookies();
		expect(cookies.filter((cookie) => cookie.name.endsWith("expo_signup"))).toEqual([]);
	});
});

test.describe("signed in", () => {
	test.use({ viewer: "member" });

	test("log out ends this device's session only", async ({ page, me }) => {
		await page.goto("/");
		await expectSignedIn(page, me.username);
		// Another device of the same account, signed in before the logout.
		expect((await me.api.get("/api/v1/me")).status()).toBe(200);

		await header(page).getByRole("button", { name: "Log out" }).click();
		await expect(page).toHaveURL("/");
		await expectSignedOut(page);

		await page.goto("/account");
		await expect(page).toHaveURL(/\/login\?next=%2Faccount/);
		// The other device stays logged in ("Log out everywhere" is on /account).
		expect((await me.api.get("/api/v1/me")).status()).toBe(200);
	});

	test("the header shows the account before the page's scripts run", async ({ page, me }) => {
		const release = await holdScripts(page);
		await page.goto("/", { waitUntil: "commit" });
		await expectSignedIn(page, me.username);
		release();
	});

	test("a page left open after logging out in another tab sends its next action to the login page", async ({
		page,
		me,
		context,
	}) => {
		await page.goto("/account");
		await expect(
			page.getByRole("heading", { level: 1, name: "Account settings" }),
		).toBeVisible();
		await ready(page);

		const other = await context.newPage();
		await other.goto("/");
		await expectSignedIn(other, me.username);
		await header(other).getByRole("button", { name: "Log out" }).click();
		await expectSignedOut(other);

		// Nothing in the browser tells the open page; the API refuses its next call.
		await page.getByRole("button", { name: "Add a passkey on this device" }).click();
		await expect(page).toHaveURL(/\/login\?next=%2Faccount$/);
		await expectSignedOut(page);
	});

	test("a session revoked by logging out everywhere elsewhere shows pages signed out", async ({
		page,
		seed,
		me,
	}) => {
		const owner = await seed.account();
		const entry = await seed.entry({ title: `Revoked view ${uniqueWord()}`, owner });
		// "Log out everywhere" on another device revokes this browser's session too.
		const logout = await me.api.delete("/api/v1/me/sessions");
		expect(logout.ok(), `log out everywhere answered ${logout.status()}`).toBe(true);

		for (const path of [`/e/${entry.id}`, `/u/${owner.username}`]) {
			const response = await page.goto(path);
			expect(response?.status(), path).toBe(200);
			await expectSignedOut(page);
		}
		// The dead cookie is gone, so the browser stops sending it.
		const cookies = await page.context().cookies();
		expect(cookies.filter((cookie) => cookie.name.endsWith("expo_session"))).toEqual([]);
	});

	test("the login and username pages send a signed-in visitor home", async ({ page, me }) => {
		await page.goto("/login");
		await expect(page).toHaveURL("/");
		await page.goto("/signup");
		await expect(page).toHaveURL("/");
		await expectSignedIn(page, me.username);
	});
});

test.describe("on a slow connection", () => {
	test("a username typed before the page's scripts arrive stays out of the URL", async ({
		page,
	}) => {
		await useVirtualAuthenticator(page);
		const release = await holdScripts(page);
		await page.goto("/login", { waitUntil: "commit" });
		const username = uniqueWord("user_");
		await expect(
			main(page).getByRole("button", { name: "Log in with a passkey" }),
		).toBeDisabled();
		// The choice is CSS only, so it works before the scripts arrive.
		await page.getByRole("radio", { name: "I'm new here" }).check();
		await page.getByLabel("Username").fill(username);

		// Without its script the form would GET-submit itself; it cannot yet.
		const submit = page.getByRole("button", { name: "Create account with a passkey" });
		await expect(submit).toBeDisabled();
		await page.getByLabel("Username").press("Enter");
		await expect(page.waitForURL(/username=/, { timeout: 2_000 })).rejects.toThrow();

		release();
		await submit.click();
		await expectSignedIn(page, username);
		expect(page.url()).not.toContain("username");
	});
});

test.describe("a session cookie alone", () => {
	test("opens account pages", async ({ page, seed }) => {
		const account = await seed.account();
		// Cookie only, never through the login page: the server alone decides who is viewing.
		await page.context().addCookies(account.session);

		await page.goto("/account");
		await expect(
			page.getByRole("heading", { level: 1, name: "Account settings" }),
		).toBeVisible();
		await expect(page.locator("[data-settings]")).toContainText(account.username);
		await expectSignedIn(page, account.username);
	});
});
