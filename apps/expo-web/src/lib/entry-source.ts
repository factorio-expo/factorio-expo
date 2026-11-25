// Where an entry came from and where else it lives: the "Imported from" link
// of an entry uploaded by import, and its vanity alias URL.
import type { Entry } from "@lib/api";

/** An imported entry's source: its link and the host shown for it (undefined for an upload or an unusable link). */
export function sourceHost(
	sourceUrl: string | undefined,
): { url: string; host: string } | undefined {
	if (!sourceUrl) return undefined;
	try {
		const url = new URL(sourceUrl);
		// The API stores only https links; a page never links anything else.
		if (url.protocol !== "https:") return undefined;
		return { url: url.toString(), host: url.hostname };
	} catch {
		return undefined;
	}
}

/** `/@{username}/{alias}`, or undefined when the entry has no alias. */
export function aliasPath(entry: Pick<Entry, "alias" | "owner_username">): string | undefined {
	if (!entry.alias || !entry.owner_username) return undefined;
	return `/@${encodeURIComponent(entry.owner_username)}/${encodeURIComponent(entry.alias)}`;
}
