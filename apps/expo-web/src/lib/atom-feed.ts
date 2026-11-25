// A user's Atom feed (pages/u/[username]/feed.atom.ts): the last
// publications and revisions of their public entries, from the API's feed
// listing. Pure (data in, XML out) so it is unit tested without a server.
import type { UserFeed } from "@factorio-expo/api-types/public";
import { stripRichText } from "@factorio-expo/ui/game/rich-text.ts";

/** Where a user's feed is served, and what their profile's `<link rel="alternate">` points at. */
export const feedPath = (username: string) => `/u/${encodeURIComponent(username)}/feed.atom`;

export const ATOM_TYPE = "application/atom+xml";

/**
 * The text with what XML 1.0 forbids taken out, even when escaped: most
 * control characters, lone surrogates and U+FFFE/U+FFFF.
 */
function stripInvalidXml(text: string): string {
	let kept = "";
	// Iterating by code point yields a lone surrogate as itself, which is then dropped.
	for (const char of text) {
		const code = char.codePointAt(0)!;
		if (
			code === 0x9 ||
			code === 0xa ||
			code === 0xd ||
			(code >= 0x20 && code <= 0xd7ff) ||
			(code >= 0xe000 && code <= 0xfffd) ||
			(code >= 0x10000 && code <= 0x10ffff)
		) {
			kept += char;
		}
	}
	return kept;
}

/** Text for an XML text node or attribute value. */
export function escapeXml(text: string): string {
	return stripInvalidXml(text)
		.replace(/&/g, "&amp;")
		.replace(/</g, "&lt;")
		.replace(/>/g, "&gt;")
		.replace(/"/g, "&quot;")
		.replace(/'/g, "&apos;");
}

/** An RFC 3339 timestamp in UTC. */
function timestamp(value: string): string {
	return new Date(value).toISOString();
}

export interface AtomFeed {
	xml: string;
	/** The feed's `updated`: its newest item's time (the account's age when it has none). */
	updated: string;
}

/**
 * The feed document. `site` is the site's origin; `joinedAt` stands in for
 * `updated` while the user has nothing in the feed. A first revision's item
 * carries the entry's description, a later one its note; nothing else is
 * ever included, so a private note or entry cannot leak through here.
 */
export function atomFeed(site: URL | string, feed: UserFeed, joinedAt: string): AtomFeed {
	const url = (path: string) => new URL(path, site).toString();
	const self = url(feedPath(feed.username));
	const profile = url(`/u/${encodeURIComponent(feed.username)}`);
	const updated = timestamp(
		feed.items.length
			? new Date(
					Math.max(...feed.items.map((item) => Date.parse(item.created_at))),
				).toISOString()
			: joinedAt,
	);

	const entries = feed.items.map((item) => {
		const title = stripRichText(item.title).replace(/\s+/g, " ").trim() || "Untitled entry";
		const time = timestamp(item.created_at);
		const link = item.first_revision
			? url(`/e/${item.entry_id}`)
			: `${url(`/e/${item.entry_id}`)}?revision=${encodeURIComponent(item.revision_id)}`;
		const heading = item.first_revision ? title : `${title}: revision ${item.version_number}`;
		const body = item.first_revision ? item.description : item.note;
		return [
			"\t<entry>",
			`\t\t<id>urn:factorio-expo:revision:${escapeXml(item.revision_id)}</id>`,
			`\t\t<title>${escapeXml(heading)}</title>`,
			`\t\t<link rel="alternate" type="text/html" href="${escapeXml(link)}"/>`,
			`\t\t<published>${time}</published>`,
			`\t\t<updated>${time}</updated>`,
			...(body ? [`\t\t<content type="text">${escapeXml(body)}</content>`] : []),
			"\t</entry>",
		].join("\n");
	});

	const xml = [
		'<?xml version="1.0" encoding="UTF-8"?>',
		'<feed xmlns="http://www.w3.org/2005/Atom">',
		`\t<id>urn:factorio-expo:feed:${escapeXml(feed.username.toLowerCase())}</id>`,
		`\t<title>${escapeXml(feed.username)} on Factorio Expo</title>`,
		`\t<subtitle>New blueprints and revisions by ${escapeXml(feed.username)}</subtitle>`,
		`\t<link rel="self" type="${ATOM_TYPE}" href="${escapeXml(self)}"/>`,
		`\t<link rel="alternate" type="text/html" href="${escapeXml(profile)}"/>`,
		`\t<updated>${updated}</updated>`,
		`\t<author><name>${escapeXml(feed.username)}</name><uri>${escapeXml(profile)}</uri></author>`,
		...entries,
		"</feed>",
		"",
	].join("\n");
	return { xml, updated };
}
