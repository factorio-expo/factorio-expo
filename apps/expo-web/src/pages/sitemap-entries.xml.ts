// Sitemap of every public entry, read from the API's sitemap listing (the
// same entries search holds, straight from PostgreSQL, 5000 per page). Listed
// from the static sitemap index through astro.config.mjs `customSitemaps`; the
// build-time sitemap cannot see them.
//
// The result is kept in memory for TTL_MS and rebuilt by the first request
// after that. Requests arriving during a rebuild wait for the same one; if a
// rebuild fails, the last good copy is served until the next request tries
// again.
import type { APIRoute } from "astro";

import { getServerApiClient } from "@lib/api/server";

const TTL_MS = 10 * 60 * 1000;
const MAX_URLS = 50_000; // sitemap protocol limit per file

let cached: { xml: string; builtAt: number } | undefined;
let rebuilding: Promise<string> | undefined;

function escapeXml(text: string): string {
	return text.replace(/[<>&'"]/g, (char) => `&#${char.charCodeAt(0)};`);
}

async function build(base: URL): Promise<string> {
	// Anonymous on purpose: the sitemap lists what anyone may see.
	const api = getServerApiClient();
	const urls: string[] = [];
	let after: string | undefined;
	do {
		// Each page names the next, so they are fetched in order.
		// oxlint-disable-next-line no-await-in-loop
		const page = await api.sitemap.entries(after);
		for (const entry of page.entries) {
			const loc = new URL(`/e/${encodeURIComponent(entry.id)}`, base).toString();
			urls.push(
				`<url><loc>${escapeXml(loc)}</loc><lastmod>${escapeXml(entry.updated_at)}</lastmod></url>`,
			);
		}
		after = page.next;
	} while (after && urls.length < MAX_URLS);
	return `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${urls.slice(0, MAX_URLS).join("")}</urlset>`;
}

/** The cached sitemap, rebuilt (once, however many requests wait) when older than TTL_MS. */
async function sitemap(base: URL): Promise<{ xml: string; builtAt: number }> {
	if (cached && performance.now() - cached.builtAt < TTL_MS) return cached;
	rebuilding ??= build(base).finally(() => {
		rebuilding = undefined;
	});
	try {
		cached = { xml: await rebuilding, builtAt: performance.now() };
	} catch (err) {
		if (!cached) throw err;
		console.error("sitemap rebuild failed; serving the previous one", err);
	}
	return cached;
}

export const GET: APIRoute = async ({ site, url }) => {
	let result;
	try {
		result = await sitemap(site ?? url);
	} catch (err) {
		console.error("sitemap build failed", err);
		return new Response("Sitemap temporarily unavailable", {
			status: 503,
			headers: { "Retry-After": "300", "Cache-Control": "no-store" },
		});
	}
	// Shared caches keep it no longer than this copy has left.
	const maxAge = Math.max(0, Math.floor((TTL_MS - (performance.now() - result.builtAt)) / 1000));
	return new Response(result.xml, {
		headers: {
			"Content-Type": "application/xml; charset=utf-8",
			"Cache-Control": `public, max-age=${maxAge}`,
		},
	});
};
