// Server-side: the site banner and paused actions (GET /api/v1/site), read for
// every page render. The answer is public and the same for every visitor, so
// one copy is shared for a few seconds; when the API cannot answer, the site
// renders as open (the action itself is refused by the API either way).
import type { SiteStatus } from "@lib/api";
import { getServerApiClient } from "@lib/api/server";
import { OPEN_SITE } from "@lib/site-pause";

const CACHE_MS = 5_000;

let cached: { at: number; status: Promise<SiteStatus> } | undefined;

/** The current site status; never rejects. */
export function getSiteStatus(): Promise<SiteStatus> {
	const now = Date.now();
	if (!cached || now - cached.at >= CACHE_MS) {
		const status = getServerApiClient()
			.site.status()
			.catch(() => OPEN_SITE);
		cached = { at: now, status };
	}
	return cached.status;
}
