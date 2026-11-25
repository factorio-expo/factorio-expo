import type { Entry, Node, Preview } from "@lib/api";
import { stripRichText } from "@factorio-expo/ui/game/rich-text.ts";
import { markdownToPlainText } from "@utils/plain-text.ts";

const MAX_DESCRIPTION = 160;

/**
 * A meta description from user Markdown: plain text without icon tokens,
 * cut at a word near 160 characters. Empty input gives the fallback.
 */
export function metaDescription(markdown: string | undefined, fallback: string): string {
	const text = stripRichText(markdownToPlainText(markdown ?? ""))
		.replace(/\s+/g, " ")
		.trim();
	if (!text) return fallback;
	if (text.length <= MAX_DESCRIPTION) return text;
	const cut = text.slice(0, MAX_DESCRIPTION - 1);
	const space = cut.lastIndexOf(" ");
	return `${(space > 100 ? cut.slice(0, space) : cut).trimEnd()}…`;
}

/** JSON for a <script type="application/ld+json">; "<" is escaped so no value can close the tag. */
export function jsonLd(data: object): string {
	return JSON.stringify(data).replace(/</g, "\\u003c");
}

export interface CreativeWorkInput {
	/** The site origin absolute URLs are built on (Astro.site). */
	site: URL;
	/** Root-relative path of the page itself. */
	path: string;
	name: string;
	description: string;
	/** The entry the page shows: its dates and uploader. */
	entry: Pick<Entry, "created_at" | "updated_at" | "owner_username">;
	/** Root-relative or absolute URL of the preview image. */
	image?: string;
	keywords?: string[];
	viewCount?: number;
	/** What the page belongs to: the site, or the book entry of a leaf. */
	isPartOf: { type: "WebSite" | "CreativeWork"; name: string; path: string };
}

/** Schema.org CreativeWork JSON-LD for an entry or one of a book's blueprints. */
export function creativeWorkJsonLd(input: CreativeWorkInput): string {
	const { site, entry } = input;
	const absolute = (path: string) => new URL(path, site).toString();
	const owner = entry.owner_username;
	return jsonLd({
		"@context": "https://schema.org",
		"@type": "CreativeWork",
		name: input.name,
		description: input.description,
		url: absolute(input.path),
		dateCreated: entry.created_at,
		dateModified: entry.updated_at,
		...(input.keywords?.length && { keywords: input.keywords.join(", ") }),
		...(input.image && { image: absolute(input.image) }),
		...(owner && {
			author: {
				"@type": "Person",
				name: owner,
				url: absolute(`/u/${encodeURIComponent(owner)}`),
			},
		}),
		...(input.viewCount !== undefined && {
			interactionStatistic: {
				"@type": "InteractionCounter",
				interactionType: "https://schema.org/ViewAction",
				userInteractionCount: input.viewCount,
			},
		}),
		isPartOf: {
			"@type": input.isPartOf.type,
			name: input.isPartOf.name,
			url: absolute(input.isPartOf.path),
		},
	});
}

/**
 * The entry's cover image, as on its card: the first leaf in depth-first tree
 * order that has a preview (catalog ListEntryCards picks the lowest position).
 */
export function coverPreview(node: Node | undefined): Preview | undefined {
	if (!node) return undefined;
	if (node.kind === "blueprint") return node.preview;
	for (const child of node.children ?? []) {
		const preview = coverPreview(child);
		if (preview) return preview;
	}
	return undefined;
}
