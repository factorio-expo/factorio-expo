// Share targets of the entry and leaf pages' Share menu. The shared address is
// always the page's own path: no query string (so an edit token in `?token=`
// is never shared) and no tracking or referral parameter.

/** The page address to share: path only, on `site`'s origin (the public one; the page's by default). */
export function shareableUrl(page: URL, site: URL = page): string {
	return new URL(page.pathname, site.origin).toString();
}

/** Reddit's own submit form, prefilled with the link and a title. */
export function redditSubmitUrl(url: string, title: string): string {
	const params = new URLSearchParams({ url, title });
	return `https://www.reddit.com/submit?${params.toString()}`;
}

/** What the browser's share sheet gets (Web Share API). */
export interface ShareData {
	url: string;
	title: string;
}

/**
 * Opens the system share sheet. Resolves false when the browser has none or
 * the visitor closed it (an AbortError is not a failure worth a message).
 */
export async function nativeShare(
	data: ShareData,
	nav: Pick<Navigator, "share"> & Partial<Pick<Navigator, "canShare">> = navigator,
): Promise<boolean> {
	if (typeof nav.share !== "function") return false;
	if (typeof nav.canShare === "function" && !nav.canShare(data)) return false;
	try {
		await nav.share(data);
		return true;
	} catch {
		return false;
	}
}
