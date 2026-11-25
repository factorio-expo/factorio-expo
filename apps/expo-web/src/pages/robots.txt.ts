// Crawl rules. Account pages need a session, so crawlers only ever see a login
// redirect there; pages that are reachable but not worth indexing (login,
// signup, unlisted entries, edit links) say noindex themselves instead, since
// a Disallow would hide that from crawlers.
import type { APIRoute } from "astro";

export const prerender = true;

export const GET: APIRoute = ({ site }) => {
	const sitemap = new URL("/sitemap-index.xml", site).toString();
	return new Response(
		[
			"User-agent: *",
			"Disallow: /account/",
			"Disallow: /e/*/revisions/new",
			"",
			`Sitemap: ${sitemap}`,
			"",
		].join("\n"),
		{ headers: { "Content-Type": "text/plain; charset=utf-8" } },
	);
};
