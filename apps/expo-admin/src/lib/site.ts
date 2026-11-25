import { ADMIN_SITE_URL } from "astro:env/server";

/** The public page of an entry (or one of its comments), when ADMIN_SITE_URL is set. */
export function siteEntryUrl(entryId: string, commentNumber?: number): string | undefined {
	if (!ADMIN_SITE_URL) return undefined;
	const url = new URL(`/e/${encodeURIComponent(entryId)}`, ADMIN_SITE_URL);
	if (commentNumber !== undefined) url.hash = `c-${commentNumber}`;
	return url.toString();
}

/** A root-relative page of the public site as a full URL, when ADMIN_SITE_URL is set. */
export function sitePageUrl(path: string): string | undefined {
	if (!ADMIN_SITE_URL || !path.startsWith("/")) return undefined;
	return new URL(path, ADMIN_SITE_URL).toString();
}
