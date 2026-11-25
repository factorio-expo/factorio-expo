/**
 * Every image/preview URL the API returns (Preview.url and thumbnail_url, the
 * cards' preview_url — see catalog.PreviewURL / ThumbnailURL in apps/expo-api)
 * is a path relative to the API's own origin, e.g.
 * "/api/v1/blueprints/{id}/thumbnail.webp". They are
 * rendered into HTML sent to a browser, so they must always resolve against
 * the *public* API origin — never the SSR-only internal one a server client
 * may be using. Call this wherever such a path is rendered into an <img src>
 * or link.
 */
export function toPublicAssetUrl(publicBaseUrl: string, path: string): string {
	if (/^https?:\/\//.test(path)) return path;
	const base = publicBaseUrl.endsWith("/") ? publicBaseUrl : `${publicBaseUrl}/`;
	const relative = path.replace(/^\//, "");
	// A relative base ("/") is the site's own origin: the URL stays root-relative.
	if (!/^https?:\/\//.test(base)) return `${base}${relative}`;
	return new URL(relative, base).toString();
}

/**
 * Raw exchange-string routes (text/plain, readable from any origin): a
 * revision's exact submitted string (the live one has its own, shorter-cached
 * route) and a leaf's own string. They answer only
 * for entries a signed-out visitor may see, so never link them on a private
 * entry. Pass the result through toPublicAssetUrl.
 */
export function entryRawPath(entryId: string): string {
	return `/api/v1/entries/${encodeURIComponent(entryId)}/raw`;
}

/** The shown revision's raw path: the entry's own route when it is the live one. */
export function shownRevisionRawPath(
	entryId: string,
	revisionId: string,
	liveRevisionId: string | undefined,
): string {
	return revisionId === liveRevisionId
		? entryRawPath(entryId)
		: revisionRawPath(entryId, revisionId);
}

export function revisionRawPath(entryId: string, revisionId: string): string {
	return `/api/v1/entries/${encodeURIComponent(entryId)}/revisions/${encodeURIComponent(revisionId)}/raw`;
}

export function blueprintRawPath(blueprintId: string): string {
	return `/api/v1/blueprints/${encodeURIComponent(blueprintId)}/raw`;
}
