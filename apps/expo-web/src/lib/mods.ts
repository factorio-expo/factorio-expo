// How a blueprint's mod detection (Go mods.Detection) reads on the page.
import type { ModDetection, Modset } from "@lib/api";
import { MOD_FILTER_OPTIONS } from "@lib/search/query";

export interface ModLabel {
	text: string;
	/** Tooltip: how sure the guess is. */
	title: string;
}

const unknownCount = (detection: ModDetection) =>
	detection.unknown_entities.length + detection.unknown_recipes.length;

/** What the blueprint needs, in the API's order: built-in mods by name,
 * other modsets with the share of modded names they define, then "unknown
 * mod" when some names no listed modset defines. */
export function modLabels(detection: ModDetection | undefined): ModLabel[] {
	if (!detection) return [];
	const labels = detection.modsets.map((match) =>
		match.builtin
			? { text: match.title, title: `Uses content from ${match.title}` }
			: {
					text: `${match.title} (${Math.round(match.score * 100)}%)`,
					title: `Probably ${match.title}: it defines ${Math.round(match.score * 100)}% of the modded names`,
				},
	);
	const unknown = unknownCount(detection);
	if (unknown > 0) {
		labels.push({
			text: "unknown mod",
			title: `${unknown} ${unknown === 1 ? "name" : "names"} no listed mod defines`,
		});
	}
	return labels;
}

/** A mod badge as a card shows it: what filters the browse page to it. */
export interface ModBadgeRef {
	/** The `mods` filter value: "space-age" or a modset name. */
	value: string;
	label: string;
	href: string;
}

/** The browse page filtered to one mod, like a tag chip's link. */
export function modHref(value: string): string {
	return `/browse?mods=${encodeURIComponent(value)}`;
}

/**
 * Badges for the filter values a result carries (`mod_badges`), titled from
 * the filter's own options and GET /api/v1/modsets; a value neither knows shows
 * as is.
 */
export function modBadgeRefs(values: string[], modsets: Modset[]): ModBadgeRef[] {
	const titles = new Map<string, string>(MOD_FILTER_OPTIONS.map((o) => [o.value, o.label]));
	for (const modset of modsets) titles.set(modset.name, modset.title);
	return values.map((value) => ({
		value,
		label: titles.get(value) ?? value,
		href: modHref(value),
	}));
}

/** Whether the blueprint uses content beyond the game's own mods, which the
 * preview and analysis leave out. */
export function usesThirdPartyMods(detection: ModDetection | undefined): boolean {
	return thirdPartyMods(detection).length > 0;
}

/** Those mods by name, best guess first, then "unknown mod" for names no
 * listed modset defines. The blueprint editor leaves them out too: it knows
 * only the game's own. */
export function thirdPartyMods(detection: ModDetection | undefined): string[] {
	if (!detection) return [];
	const names = detection.modsets.filter((match) => !match.builtin).map((match) => match.title);
	if (unknownCount(detection) > 0) names.push("unknown mod");
	return names;
}
