// The one reader of an entry details form (EntryDetailsFields): upload,
// publish and the inline edit form all turn their fields into API input here.
// Pure (FormData in, plain object out), so it is unit tested without a DOM.
import type { Visibility } from "@lib/api/types";

/**
 * Hidden field EntryDetailsFields renders beside the tag picker. Without it
 * the picker was left out (the tag list failed to load), so the reader must
 * not send `tags`: an empty list would remove every tag.
 */
export const TAG_PICKER_FIELD = "tag-picker";

/** Hidden field beside the spoiler checkbox: without it the form sends no `spoiler`. */
export const SPOILER_FIELD = "spoiler-field";

export interface EntryDetails {
	title: string;
	description: string;
	/** Absent when the form had no tag picker: the entry keeps its tags. */
	tags?: string[];
	/** Absent when the form had no visibility select (anonymous entries). */
	visibility?: Visibility;
	/** Lifetime in days; absent for "keep" and for a form without an expiry select. */
	expires_in_days?: number;
	/** Spoiler/NSFW; absent for a form without the checkbox. */
	spoiler?: boolean;
	/** The vanity alias, "" for none; absent for a form without the field (anonymous entries). */
	alias?: string;
}

const isVisibility = (value: unknown): value is Visibility =>
	value === "public" || value === "unlisted" || value === "private";

const text = (data: FormData, name: string) => {
	const value = data.get(name);
	return typeof value === "string" ? value.trim() : "";
};

/** Reads the entry details a form submitted, sending only the fields it rendered. */
export function readEntryDetails(data: FormData): EntryDetails {
	const details: EntryDetails = {
		title: text(data, "title"),
		description: text(data, "description"),
	};
	if (data.has(TAG_PICKER_FIELD)) {
		details.tags = data.getAll("tags").filter((tag): tag is string => typeof tag === "string");
	}
	const visibility = data.get("visibility");
	if (isVisibility(visibility)) details.visibility = visibility;
	const expiry = data.get("expires_in_days");
	if (typeof expiry === "string" && expiry !== "") details.expires_in_days = Number(expiry);
	if (data.has(SPOILER_FIELD)) details.spoiler = data.get("spoiler") === "on";
	if (data.has("alias")) details.alias = text(data, "alias").toLowerCase();
	return details;
}

/**
 * The question to ask before a change that deletes the entry's comments, or
 * undefined when nothing is lost: only public entries keep a thread.
 */
export function commentPurgeWarning(
	visibility: Visibility | undefined,
	commentCount: number,
): string | undefined {
	if (!visibility || visibility === "public" || !(commentCount > 0)) return undefined;
	const comments = `${commentCount} comment${commentCount === 1 ? "" : "s"}`;
	return `Only public entries keep comments: making it ${visibility} deletes its ${comments} for good. Continue?`;
}
