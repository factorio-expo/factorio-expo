// Who sees, shares, indexes or reports an entry, decided once from the entry
// and the viewer's rights (entry-access.ts). The entry page, the leaf page and
// EntryInfo only read these flags, so they agree by construction. Pure.
import type { Entry } from "@lib/api";

export interface EntryView {
	/** Search engines may index it: public and not a quick preview. */
	indexable: boolean;
	/** Offer the share button and a raw exchange-string link: nothing to share when private. */
	shareable: boolean;
	/** The page's views are counted (visitors only, see `countsViews`). */
	countsViews: boolean;
	/** The page sends the view beacon: counted views, never from editors. */
	sendsViewBeacon: boolean;
	/** The viewer may report it: not its editor, not a quick preview. */
	reportable: boolean;
	/** The owner may pin it to their profile. */
	pinnable: boolean;
	/** A reminder for unlisted and private entries; public is the default and goes unsaid. */
	visibilityNote?: string;
	/** Spoilers keep their previews hidden until a click, so they get no share image. */
	showShareImage: boolean;
}

const VISIBILITY_NOTES = {
	public: undefined,
	unlisted: "Unlisted: only people with the link can see it",
	private: "Private: only you can see it",
} as const;

export function entryView(entry: Entry, access: { canEdit: boolean }): EntryView {
	const preview = entry.quick_preview === true;
	// Views are counted for visitors only: not quick previews or private
	// entries, which only their owner sees.
	const countsViews = !preview && entry.visibility !== "private";
	return {
		indexable: !preview && entry.visibility === "public",
		shareable: entry.visibility !== "private",
		countsViews,
		sendsViewBeacon: countsViews && !access.canEdit,
		reportable: !access.canEdit && !preview,
		// An owned entry's editor is its owner; anonymous entries have no profile.
		pinnable:
			access.canEdit &&
			Boolean(entry.owner_user_id) &&
			entry.visibility === "public" &&
			!preview,
		visibilityNote: VISIBILITY_NOTES[entry.visibility],
		showShareImage: !entry.spoiler,
	};
}
