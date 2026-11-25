// A user's Atom feed: the last 30 publications and revisions of their live
// public entries, a revision with its note. Public data only, read
// anonymously (never with the visitor's session or an edit token), so the
// answer is the same for everyone and a shared cache may keep it a few minutes.
// The data is the API's `GET /api/v1/users/{username}/feed`; lib/atom-feed.ts
// builds the document.
import type { APIRoute } from "astro";
import { createHash } from "node:crypto";

import { ApiError } from "@lib/api";
import { getServerApiClient } from "@lib/api/server";
import { ATOM_TYPE, atomFeed } from "@lib/atom-feed";

/** How long readers and shared caches may keep the feed without asking again. */
const MAX_AGE_SECONDS = 300;

export const GET: APIRoute = async ({ params, request, site, url }) => {
	const api = getServerApiClient();
	let feed;
	let user;
	try {
		[feed, user] = await Promise.all([
			api.users.feed(params.username!),
			api.users.get(params.username!),
		]);
	} catch (err) {
		if (err instanceof ApiError && err.status === 404) {
			return new Response("No such user", {
				status: 404,
				headers: {
					"Content-Type": "text/plain; charset=utf-8",
					"Cache-Control": "public, max-age=60",
				},
			});
		}
		console.error("feed failed", err);
		return new Response("The feed is temporarily unavailable", {
			status: 503,
			headers: { "Retry-After": "60", "Cache-Control": "no-store" },
		});
	}
	const { xml, updated } = atomFeed(site ?? url, feed, user.created_at);
	const etag = `"${createHash("sha256").update(xml).digest("hex").slice(0, 32)}"`;
	const headers = {
		ETag: etag,
		"Last-Modified": new Date(updated).toUTCString(),
		"Cache-Control": `public, max-age=${MAX_AGE_SECONDS}`,
	};
	if (request.headers.get("If-None-Match") === etag) {
		return new Response(null, { status: 304, headers });
	}
	return new Response(xml, {
		headers: { ...headers, "Content-Type": `${ATOM_TYPE}; charset=utf-8` },
	});
};
