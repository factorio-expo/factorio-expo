// Browser-side API client factory. Import this only from client-side script
// (e.g. inside a <script> tag or client:* island) — astro:env/client only
// exposes "client" context variables and this always targets the public URL.
import { PUBLIC_API_BASE_URL } from "astro:env/client";

import { editTokenOf } from "@lib/entry-links";

import { createApiClient } from "./index";

/**
 * The API's absolute base URL for this page: PUBLIC_API_BASE_URL, resolved
 * against the page's own origin when it is relative ("/").
 */
export const browserApiBase = () => new URL(PUBLIC_API_BASE_URL, location.href).toString();

/**
 * The entry's progress stream (`GET /api/v1/entries/{id}/events`, an
 * EventSource: lib/entry-progress.ts). It takes no edit token: whoever may see
 * the entry may follow it.
 */
export const entryEventsUrl = (entryId: string) =>
	new URL(`/api/v1/entries/${encodeURIComponent(entryId)}/events`, browserApiBase()).toString();

/**
 * The session is an HttpOnly cookie the browser attaches by itself
 * (credentials: "include"); page scripts never handle the token. An edit
 * token in the page URL travels with every request (lib/entry-links.ts).
 */
export function getBrowserApiClient() {
	return createApiClient({
		baseUrl: browserApiBase(),
		editToken: editTokenOf(location.href),
		credentials: "include",
	});
}
