import type { TagGroup } from "./api";

/** A tag as shown: its curated name, the slug that filters the browse page, and its icon token (empty for none). */
export interface TagRef {
	slug: string;
	name: string;
	icon: string;
}

/**
 * Names for tag slugs (entries and search results carry slugs). A slug missing
 * from the taxonomy is shown as is.
 */
export function tagRefs(groups: TagGroup[], slugs: string[]): TagRef[] {
	const known = new Map<string, TagRef>();
	for (const group of groups) {
		for (const tag of group.tags) {
			known.set(tag.slug, { slug: tag.slug, name: tag.display_name, icon: tag.icon });
		}
	}
	return slugs.map((slug) => known.get(slug) ?? { slug, name: slug, icon: "" });
}

/** The browse page filtered to one tag. */
export function tagHref(slug: string): string {
	return `/browse?tags=${encodeURIComponent(slug)}`;
}
