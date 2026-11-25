import type {
	BoardRecord,
	Compat21,
	EntrySummary,
	NodeKind,
	SearchResultCard,
	Visibility,
} from "@lib/api";
import { entryPath, leafPath, nodeAnchor } from "@lib/entry-links";
import type { ModBadgeRef } from "@lib/mods";
import { versionBadge, type VersionBadge } from "@lib/version-badge";
import type { TagRef } from "@lib/tags";

/** What EntryCard renders; search results fill every field, listings fewer. */
export interface EntryCardData {
	entry_id: string;
	/**
	 * A search result's blueprint or planner. Inside a book (book_title), the
	 * card opens the blueprint's page, or the planner's row on the entry page.
	 */
	node_id?: string;
	/** The book the node is in; the card's header links to the book's entry. */
	book_title?: string;
	title: string;
	description: string;
	preview_url?: string;
	/** A book's collage: the card shows these thumbnails as a mosaic instead of `preview_url`. */
	cover_urls?: string[];
	/** No cover yet, but a preview is still rendering: the card shows a spinner. */
	preview_pending?: boolean;
	/** The node's kind and icons, drawn beside the title. */
	kind: NodeKind;
	icons: string;
	tags?: TagRef[];
	/** Mod badges (Space Age, guessed mods), each linking to its search; search results only. */
	mod_badges?: ModBadgeRef[];
	owner_username?: string;
	favorites_count: number;
	/** All-time page views. */
	view_count: number;
	/** Whether the viewer favorited it (every listing reports it). */
	is_favorited: boolean;
	/** Whether the viewer pinned it to their profile (listings only). */
	is_pinned?: boolean;
	/** Listings of the viewer's own entries: public ones go unsaid, the others get a label. */
	visibility?: Visibility;
	/** Set on an entry uploaded with a lifetime. */
	expires_at?: string;
	/** Spoiler/NSFW: the preview stays blurred until clicked. */
	spoiler?: boolean;
	/** For the preview's Factorio version badge (`versionBadge`). */
	game_version?: string;
	compat_21?: Compat21;
	/**
	 * The leaderboard boards it leads now (a book: any of its blueprints): a
	 * trophy chip on the preview and a primary-colour outline on the card.
	 */
	records?: BoardRecord[];
}

const UNTITLED = "Untitled blueprint";

/** What a search result needs resolved before it renders: slugs and badge values to names. */
export interface CardLookups {
	tags: (slugs: string[]) => TagRef[];
	modBadges: (values: string[]) => ModBadgeRef[];
}

/** A listing row (profile, collection, challenge) or a search result. */
export type CardSource = EntrySummary | SearchResultCard;

const isSearchResult = (source: CardSource): source is SearchResultCard => "entry_id" in source;

/**
 * The one adapter from an API row to what EntryCard renders. A new card field
 * is one edit here. Search results need `lookups` to resolve tag and mod names;
 * listing rows carry neither.
 */
export function toCard(source: CardSource, lookups?: CardLookups): EntryCardData {
	if (isSearchResult(source)) {
		return {
			entry_id: source.entry_id,
			node_id: source.node_id,
			book_title: source.book_title,
			title: source.title || UNTITLED,
			description: source.description,
			preview_url: source.preview_url,
			cover_urls: source.cover_urls,
			preview_pending: source.preview_pending,
			kind: source.kind,
			icons: source.icons,
			tags: lookups?.tags(source.tags),
			mod_badges: lookups?.modBadges(source.mod_badges),
			owner_username: source.owner_username,
			favorites_count: source.favorites_count,
			view_count: source.view_count,
			is_favorited: source.is_favorited,
			expires_at: source.expires_at,
			spoiler: source.spoiler,
			game_version: source.game_version,
			compat_21: source.compat_21,
			records: source.records,
		};
	}
	return {
		entry_id: source.id,
		title: source.title || source.label || UNTITLED,
		description: source.description,
		preview_url: source.preview_url,
		cover_urls: source.cover_urls,
		preview_pending: source.preview_pending,
		kind: source.kind,
		icons: source.icons,
		owner_username: source.owner_username,
		favorites_count: source.favorites_count,
		view_count: source.view_count,
		is_favorited: source.is_favorited,
		is_pinned: source.is_pinned,
		visibility: source.visibility,
		expires_at: source.expires_at,
		spoiler: source.spoiler,
		game_version: source.game_version,
		compat_21: source.compat_21,
		records: source.records,
	};
}

/** What EntryCard derives from its data before it renders anything. */
export interface CardView {
	/** Where the card opens: the entry, a book's blueprint page or a planner's row. */
	href: string;
	/** The book's entry, for the header of a card inside a book. */
	bookHref?: string;
	/** The cover image's public URL. */
	previewUrl?: string;
	/** A book's collage (two or more thumbnails), shown instead of the single cover. */
	mosaic: string[];
	badge?: VersionBadge;
	/** The first board it leads, for the trophy chip; and every board, for its tooltip. */
	record?: BoardRecord;
	/** How many more boards it leads ("+2" beside the chip). */
	moreRecords: number;
	recordTitle: string;
}

/**
 * Everything EntryCard decides about a card: its links, cover, version
 * badge and record chip. `asset` turns an API path into a public URL
 * (lib/api/public-asset.ts).
 */
export function cardView(card: EntryCardData, asset: (path: string) => string): CardView {
	const inBook = Boolean(card.book_title && card.node_id);
	let href = entryPath(card.entry_id);
	if (inBook && card.node_id) {
		href =
			card.kind === "blueprint"
				? leafPath(card.entry_id, card.node_id)
				: entryPath(card.entry_id) + nodeAnchor(card.node_id);
	}
	const previewUrl = card.preview_url ? asset(card.preview_url) : undefined;
	const covers = (card.cover_urls ?? []).map(asset);
	const records = card.records ?? [];
	return {
		href,
		bookHref: inBook ? entryPath(card.entry_id) : undefined,
		previewUrl,
		mosaic: previewUrl && covers.length >= 2 ? covers : [],
		badge: versionBadge(card.game_version, card.compat_21),
		record: records[0],
		moreRecords: Math.max(0, records.length - 1),
		recordTitle: records.map((held) => `Holds the ${held.title} record`).join("; "),
	};
}
