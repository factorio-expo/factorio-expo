// OpenSearch description: lets a browser add the site's search (the browse
// page's `q`) as a search engine. Prerendered like robots.txt; Page.astro
// links it from every page's head (<link rel="search">).
import type { APIRoute } from "astro";
import { openSearchDescription } from "@lib/opensearch";

export const prerender = true;

export const GET: APIRoute = ({ site, url }) =>
	new Response(openSearchDescription(site ?? url), {
		headers: { "Content-Type": "application/opensearchdescription+xml; charset=utf-8" },
	});
