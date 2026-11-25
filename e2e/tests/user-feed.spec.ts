// A user's Atom feed (/u/{name}/feed.atom): the last publications and
// revisions of their live public entries, a revision with its note. Never
// private, unlisted or anonymous entries.
import { request, type Page } from "@playwright/test";
import { uniqueWord } from "../support/api.ts";
import { BLUEPRINT, OTHER_BLUEPRINT, relabel } from "../support/blueprints.ts";
import { expect, test } from "../support/fixtures.ts";
import { webURL } from "../support/stack.ts";

/** Parses the document the way a feed reader's XML parser would; a parse error is reported. */
async function parsedFeed(page: Page, path: string) {
	await page.goto("/about");
	return page.evaluate(async (feedPath) => {
		const response = await fetch(feedPath);
		const text = await response.text();
		const document = new DOMParser().parseFromString(text, "application/xml");
		const ns = "http://www.w3.org/2005/Atom";
		const first = (parent: Element | Document, name: string) =>
			parent.getElementsByTagNameNS(ns, name)[0];
		return {
			status: response.status,
			contentType: response.headers.get("content-type"),
			etag: response.headers.get("etag"),
			lastModified: response.headers.get("last-modified"),
			cacheControl: response.headers.get("cache-control"),
			parseError: document.getElementsByTagName("parsererror").length > 0,
			root: document.documentElement.localName,
			updated: first(document.documentElement, "updated")?.textContent,
			entries: Array.from(document.getElementsByTagNameNS(ns, "entry")).map((entry) => ({
				id: first(entry, "id")?.textContent,
				title: first(entry, "title")?.textContent,
				updated: first(entry, "updated")?.textContent,
				content: first(entry, "content")?.textContent ?? null,
				href: first(entry, "link")?.getAttribute("href"),
			})),
		};
	}, path);
}

test.describe("as member", () => {
	test.use({ viewer: "member" });

	test("lists publications and revisions with their notes, public entries only", async ({
		page,
		seed,
		me,
	}) => {
		const word = uniqueWord();
		const title = `Feed <${word}> & more`;
		const entry = await seed.entry({ owner: me, title, description: `Describes ${word}` });
		const note = `Fixed the belts & inserters ${word}`;
		await seed.revision(entry, relabel(OTHER_BLUEPRINT, uniqueWord()), me, note);
		await seed.revision(entry, relabel(BLUEPRINT, uniqueWord()), me);
		const unlisted = await seed.entry({
			owner: me,
			title: `Unlisted ${word}`,
			visibility: "unlisted",
		});
		const hidden = await seed.entry({
			owner: me,
			title: `Private ${word}`,
			visibility: "private",
		});
		await seed.revision(
			hidden,
			relabel(OTHER_BLUEPRINT, uniqueWord()),
			me,
			`Private note ${word}`,
		);

		const feed = await parsedFeed(page, `/u/${me.username}/feed.atom`);
		expect(feed.status).toBe(200);
		expect(feed.contentType).toContain("application/atom+xml");
		expect(feed.parseError).toBe(false);
		expect(feed.root).toBe("feed");
		expect(feed.etag).toBeTruthy();
		expect(feed.lastModified).toBeTruthy();

		// Newest first: the third revision (no note), the second (a note), the entry itself.
		expect(feed.entries).toHaveLength(3);
		const [third, second, published] = feed.entries;
		expect(third?.title).toBe(`Feed <${word}> & more: revision 3`);
		expect(third?.content).toBeNull();
		expect(second?.title).toBe(`Feed <${word}> & more: revision 2`);
		expect(second?.content).toBe(note);
		expect(second?.href).toContain(`/e/${entry.id}?revision=`);
		expect(published?.title).toBe(title);
		expect(published?.content).toBe(`Describes ${word}`);
		expect(published?.href).toMatch(new RegExp(`/e/${entry.id}$`));
		for (const item of feed.entries) {
			expect(item.id).toMatch(/^urn:factorio-expo:revision:/);
			expect(Number.isNaN(Date.parse(item.updated ?? ""))).toBe(false);
		}

		// Nothing private or unlisted in the raw document either.
		const raw = await page.request.get(`/u/${me.username}/feed.atom`);
		const body = await raw.text();
		for (const secret of [
			unlisted.title,
			hidden.title,
			`Private note ${word}`,
			unlisted.id,
			hidden.id,
		]) {
			expect(body).not.toContain(secret);
		}
		expect(body).toContain('<link rel="self" type="application/atom+xml" href="');
	});

	test("answers a repeated request with 304 and 404 for an unknown user", async ({
		seed,
		me,
	}) => {
		await seed.entry({ owner: me });
		const url = `/u/${me.username}/feed.atom`;
		// A feed reader sends no cookies: the answer may be kept by shared caches.
		const reader = await request.newContext({ baseURL: webURL });
		try {
			const first = await reader.get(url);
			expect(first.headers()["cache-control"]).toMatch(/^public, max-age=\d+$/);
			expect(first.headers()["last-modified"]).toBeTruthy();
			const etag = first.headers()["etag"];
			expect(etag).toBeTruthy();
			const again = await reader.get(url, { headers: { "If-None-Match": etag } });
			expect(again.status()).toBe(304);
			expect(await again.text()).toBe("");

			const missing = await reader.get(`/u/nobody_${uniqueWord()}/feed.atom`);
			expect(missing.status()).toBe(404);
		} finally {
			await reader.dispose();
		}
	});

	test("the profile advertises its feed, and an empty feed is still valid", async ({
		page,
		me,
	}) => {
		await page.goto(`/u/${me.username}`);
		const alternate = page.locator('head link[rel="alternate"][type="application/atom+xml"]');
		await expect(alternate).toHaveAttribute(
			"href",
			new RegExp(`/u/${me.username}/feed\\.atom$`),
		);

		// And a feed icon next to the name.
		await expect(
			page.getByRole("link", { name: `Atom feed of ${me.username}` }),
		).toHaveAttribute("href", `/u/${me.username}/feed.atom`);

		const feed = await parsedFeed(page, `/u/${me.username}/feed.atom`);
		expect(feed.status).toBe(200);
		expect(feed.parseError).toBe(false);
		expect(feed.entries).toHaveLength(0);
		expect(feed.updated).toBeTruthy();
	});
});

test("a deleted entry leaves the feed", async ({ page, seed }) => {
	const owner = await seed.account();
	const kept = await seed.entry({ owner, title: `Kept ${uniqueWord()}` });
	const gone = await seed.entry({ owner, title: `Gone ${uniqueWord()}` });
	await seed.deleteEntry(gone, owner);
	const feed = await parsedFeed(page, `/u/${owner.username}/feed.atom`);
	expect(feed.entries.map((item) => item.title)).toEqual([kept.title]);
});
