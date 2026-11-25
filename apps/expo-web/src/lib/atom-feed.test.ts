import type { UserFeed } from "@factorio-expo/api-types/public";
import { describe, expect, it } from "vitest";
import { atomFeed, escapeXml, feedPath } from "./atom-feed";

const site = "https://expo.example";
const feed = (items: UserFeed["items"], username = "alice"): UserFeed => ({ username, items });

describe("feedPath", () => {
	it("is under /users and encodes the name", () => {
		expect(feedPath("alice")).toBe("/u/alice/feed.atom");
		expect(feedPath("a b")).toBe("/u/a%20b/feed.atom");
	});
});

describe("escapeXml", () => {
	it("escapes markup characters and drops what XML 1.0 forbids", () => {
		expect(escapeXml(`<a href="x">Tom & 'Jerry'</a>`)).toBe(
			"&lt;a href=&quot;x&quot;&gt;Tom &amp; &apos;Jerry&apos;&lt;/a&gt;",
		);
		expect(escapeXml("a\u0000b\u0007c\u000Bd￾e")).toBe("abcde");
		expect(escapeXml("lone \ud800 high, lone \udc00 low")).toBe("lone  high, lone  low");
		expect(escapeXml("tab\tnewline\ncr\rok 🏭")).toBe("tab\tnewline\ncr\rok 🏭");
	});
});

describe("atomFeed", () => {
	const items: UserFeed["items"] = [
		{
			entry_id: "AAAAAAAAAAAA",
			title: "Smelter [item=iron-plate] <b>",
			revision_id: "RRRRRRRRRRR2",
			version_number: 2,
			first_revision: false,
			note: "Fixed the belts & inserters",
			created_at: "2026-11-14T10:00:00.123456+01:00",
		},
		{
			entry_id: "AAAAAAAAAAAA",
			title: "Smelter [item=iron-plate] <b>",
			revision_id: "RRRRRRRRRRR1",
			version_number: 1,
			first_revision: true,
			description: "Iron plates.\n\nUses *belts*.",
			created_at: "2026-11-01T08:00:00Z",
		},
	];

	it("lists a revision with its note and a first revision with the entry's description", () => {
		const { xml, updated } = atomFeed(site, feed(items), "2026-01-01T00:00:00Z");
		expect(updated).toBe("2026-11-14T09:00:00.123Z");
		expect(
			xml.startsWith(
				'<?xml version="1.0" encoding="UTF-8"?>\n<feed xmlns="http://www.w3.org/2005/Atom">',
			),
		).toBe(true);
		expect(xml).toContain("<id>urn:factorio-expo:feed:alice</id>");
		expect(xml).toContain(
			'<link rel="self" type="application/atom+xml" href="https://expo.example/u/alice/feed.atom"/>',
		);
		expect(xml).toContain(
			'<link rel="alternate" type="text/html" href="https://expo.example/u/alice"/>',
		);
		expect(xml).toContain("<updated>2026-11-14T09:00:00.123Z</updated>");
		// The newest item: a later revision, its note as content.
		expect(xml).toContain("<id>urn:factorio-expo:revision:RRRRRRRRRRR2</id>");
		expect(xml).toContain('href="https://expo.example/e/AAAAAAAAAAAA?revision=RRRRRRRRRRR2"');
		expect(xml).toContain('<content type="text">Fixed the belts &amp; inserters</content>');
		expect(xml).toContain("<title>Smelter &lt;b&gt;: revision 2</title>");
		// The entry itself.
		expect(xml).toContain(
			'<link rel="alternate" type="text/html" href="https://expo.example/e/AAAAAAAAAAAA"/>',
		);
		expect(xml).toContain('<content type="text">Iron plates.\n\nUses *belts*.</content>');
		expect(xml).toContain("<title>Smelter &lt;b&gt;</title>");
		expect(xml).toContain("<published>2026-11-01T08:00:00.000Z</published>");
		// Raw markup never reaches the document.
		expect(xml).not.toContain("<b>");
		expect(xml).not.toContain("[item=");
		expect(xml.match(/<entry>/g)).toHaveLength(2);
	});

	it("leaves out the content of a revision without a note", () => {
		const bare = { ...items[0], note: undefined };
		const { xml } = atomFeed(site, feed([bare]), "2026-01-01T00:00:00Z");
		expect(xml).not.toContain("<content");
	});

	it("is valid with no items: updated falls back to when the account was made", () => {
		const { xml, updated } = atomFeed(site, feed([]), "2026-01-01T00:00:00Z");
		expect(updated).toBe("2026-01-01T00:00:00.000Z");
		expect(xml).toContain("<updated>2026-01-01T00:00:00.000Z</updated>");
		expect(xml).not.toContain("<entry>");
		expect(xml.endsWith("</feed>\n")).toBe(true);
	});

	it("escapes the user name everywhere it appears", () => {
		const { xml } = atomFeed(site, feed([], "a&b"), "2026-01-01T00:00:00Z");
		expect(xml).toContain("<title>a&amp;b on Factorio Expo</title>");
		expect(xml).toContain("/u/a%26b/feed.atom");
		expect(xml).not.toContain("a&b");
	});

	it("names an entry without a title", () => {
		const { xml } = atomFeed(site, feed([{ ...items[1], title: "" }]), "2026-01-01T00:00:00Z");
		expect(xml).toContain("<title>Untitled entry</title>");
	});
});
