// What search engines and link previews read: page metadata, structured data,
// robots.txt and the sitemaps.
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, BOOK } from "../support/blueprints.ts";
import { apiURL } from "../support/stack.ts";
import { expect, test } from "../support/fixtures.ts";
import { bookLeaf } from "../support/ui.ts";

const SITE = "https://example.org";

test("a public entry has its description, canonical link and structured data", async ({
	page,
	seed,
}) => {
	const entry = await seed.entry({
		title: `Smelter ${uniqueWord()}`,
		description: "## Fast\nA **compact** [item=iron-plate] smelter.",
	});

	await page.goto(`/e/${entry.id}`);
	await expect(page).toHaveTitle(`${entry.title} · Factorio Expo`);
	await expect(page.locator('meta[name="description"]')).toHaveAttribute(
		"content",
		"Fast A compact smelter.",
	);
	await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
		"href",
		`${SITE}/e/${entry.id}`,
	);
	await expect(page.locator('meta[property="og:title"]')).toHaveAttribute(
		"content",
		`${entry.title} · Factorio Expo`,
	);
	await expect(page.locator('meta[name="robots"]')).toHaveCount(0);
	const data = JSON.parse(
		(await page.locator('script[type="application/ld+json"]').textContent()) ?? "",
	) as Record<string, unknown>;
	expect(data).toMatchObject({
		"@type": "CreativeWork",
		name: entry.title,
		url: `${SITE}/e/${entry.id}`,
	});
	await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
});

test("a book's blueprint page has its own structured data, part of the entry", async ({
	page,
	seed,
}) => {
	const entry = await seed.entry({ title: `Book ${uniqueWord()}`, blueprint: BOOK });

	await page.goto(`/e/${entry.id}`);
	await (await bookLeaf(page, "Assembler 2 cell")).click();
	await expect(page).toHaveURL(new RegExp(`/e/${entry.id}/b/[0-9A-Za-z]{12}`));
	const leafURL = `${SITE}${new URL(page.url()).pathname}`;
	await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", leafURL);
	const data = JSON.parse(
		(await page.locator('script[type="application/ld+json"]').textContent()) ?? "",
	) as Record<string, unknown>;
	expect(data).toMatchObject({
		"@type": "CreativeWork",
		name: "Assembler 2 cell",
		url: leafURL,
		isPartOf: { name: entry.title, url: `${SITE}/e/${entry.id}` },
	});
});

test("a single blueprint's leaf page defers to the entry page", async ({ page, seed }) => {
	const entry = await seed.entry({ blueprint: BLUEPRINT });

	const response = await page.request.get(`${apiURL}/api/v1/entries/${entry.id}`);
	const { revision } = (await response.json()) as { revision: { root: { id: string } } };

	await page.goto(`/e/${entry.id}/b/${revision.root.id}`);
	await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
		"href",
		`${SITE}/e/${entry.id}`,
	);
	await expect(page.locator('script[type="application/ld+json"]')).toHaveCount(0);
});

test("an unlisted entry is kept out of search engines", async ({ page, seed }) => {
	// Only an owned entry can be unlisted.
	const owner = await seed.account();
	const entry = await seed.entry({ visibility: "unlisted", owner });

	const response = await page.goto(`/e/${entry.id}`);
	expect(response?.headers()["x-robots-tag"]).toBe("noindex, nofollow");
	await expect(page.locator('meta[name="robots"]')).toHaveAttribute(
		"content",
		"noindex, nofollow",
	);
	await expect(page.locator('script[type="application/ld+json"]')).toHaveCount(0);
});

test("the sitemap lists searchable entries and robots.txt points to it", async ({ page, seed }) => {
	const entry = await seed.entry({ title: `Sitemap ${uniqueWord()}` });
	await seed.searchable(entry);

	const robots = await (await page.request.get("/robots.txt")).text();
	expect(robots).toContain("Disallow: /account/");
	expect(robots).toContain(`Sitemap: ${SITE}/sitemap-index.xml`);

	const index = await (await page.request.get("/sitemap-index.xml")).text();
	expect(index).toContain(`${SITE}/sitemap-entries.xml`);
	const entries = await page.request.get("/sitemap-entries.xml");
	expect(entries.ok()).toBe(true);
	// Cached in memory for 10 minutes: an earlier run on a reused stack may have built
	// it before this entry existed, so only check that it lists entries.
	expect(await entries.text()).toMatch(new RegExp(String.raw`<loc>${SITE}/e/[\w-]{12}</loc>`));
	// The second request is served from the cache with a shorter lifetime left.
	const again = await page.request.get("/sitemap-entries.xml");
	expect(again.headers()["cache-control"]).toMatch(/^public, max-age=\d+$/);
});

test("search result pages are not indexed but the browse page is", async ({ page }) => {
	await page.goto("/browse");
	await expect(page.getByRole("heading", { level: 1, name: "Browse entries" })).toBeVisible();
	await expect(page.locator('meta[name="robots"]')).toHaveCount(0);

	await page.goto(`/browse?q=${uniqueWord()}`);
	await expect(page.locator('meta[name="robots"]')).toHaveAttribute(
		"content",
		"noindex, nofollow",
	);
	await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", `${SITE}/browse`);
});

test("the legal pages are reachable from the footer, the login page and the sign-up page", async ({
	page,
}) => {
	await page.goto("/about");
	const footer = page.getByRole("contentinfo");
	await footer.getByRole("link", { name: "Privacy policy" }).click();
	await expect(page).toHaveURL(/\/privacy-policy$/);
	await expect(page.getByRole("heading", { level: 1, name: "Privacy policy" })).toBeVisible();
	await expect(page.getByRole("cell", { name: "__Host-expo_session" })).toBeVisible();
	// PUBLIC_LEGAL (e2e/compose.yaml) sets only the contact address.
	await expect(
		page.getByRole("main").getByRole("link", { name: "contact@e2e.test" }).first(),
	).toHaveAttribute("href", "mailto:contact@e2e.test");
	await expect(page.getByRole("main")).toContainText("The controller is [OWNER: full name].");

	await footer.getByRole("link", { name: "Terms of use" }).click();
	await expect(page).toHaveURL(/\/terms-of-use$/);
	await expect(page.getByRole("heading", { level: 2, name: "Fair use policy" })).toBeVisible();

	for (const path of ["/login", "/signup"]) {
		await page.goto(path);
		await expect(
			page.getByRole("main").getByRole("link", { name: "privacy policy" }),
		).toHaveAttribute("href", "/privacy-policy");
	}
});

test("the upload and preview forms link the fair use policy", async ({ page }) => {
	for (const path of ["/upload", "/preview"]) {
		await page.goto(path);
		await expect(
			page.getByRole("main").getByRole("link", { name: "fair use policy" }),
		).toHaveAttribute("href", "/terms-of-use#fair-use");
	}
});

test("every page links the OpenSearch description of the browse page's search", async ({
	page,
}) => {
	await page.goto("/about");
	await expect(page.locator('link[rel="search"]')).toHaveAttribute("href", "/opensearch.xml");
	const description = await page.request.get("/opensearch.xml");
	expect(description.ok()).toBe(true);
	const xml = await description.text();
	expect(xml).toContain("<ShortName>Factorio Expo</ShortName>");
	expect(xml).toContain(`template="${SITE}/browse?q={searchTerms}"`);
});

test("security.txt and the passkey endpoints are published under /.well-known", async ({
	page,
}) => {
	const security = await page.request.get("/.well-known/security.txt");
	expect(security.headers()["content-type"]).toMatch(/^text\/plain/);
	const text = await security.text();
	// PUBLIC_LEGAL in e2e/compose.yaml.
	expect(text).toMatch(/^Contact: mailto:contact@e2e\.test$/m);
	expect(text).toContain(`Canonical: ${SITE}/.well-known/security.txt`);
	const expires = Date.parse(/^Expires: (.+)$/m.exec(text)![1]);
	expect(expires).toBeGreaterThan(Date.now());

	const passkeys = await page.request.get("/.well-known/passkey-endpoints");
	expect(passkeys.headers()["content-type"]).toMatch(/^application\/json/);
	expect(await passkeys.json()).toEqual({
		enroll: `${SITE}/account#sign-in-methods`,
		manage: `${SITE}/account#sign-in-methods`,
	});
});
