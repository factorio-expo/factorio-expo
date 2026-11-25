// Upgrade and deconstruction planners as the site shows them: kind names,
// filter labels and the plain-text settings lines (docs/frontend.md "Planners").
import type { DeconstructionSettings, PlannerFilter, PlannerKind } from "@lib/api";

export function isPlannerKind(kind: string): kind is PlannerKind {
	return kind === "upgrade_planner" || kind === "deconstruction_planner";
}

/** "Upgrade planner", "Blueprint book" and so on. */
export function nodeKindLabel(kind: string): string {
	switch (kind) {
		case "book":
			return "Blueprint book";
		case "upgrade_planner":
			return "Upgrade planner";
		case "deconstruction_planner":
			return "Deconstruction planner";
		default:
			return "Blueprint";
	}
}

/** The item a node kind is drawn with. */
export function nodeKindItem(kind: string): string {
	switch (kind) {
		case "book":
			return "blueprint-book";
		case "upgrade_planner":
			return "upgrade-planner";
		case "deconstruction_planner":
			return "deconstruction-planner";
		default:
			return "blueprint";
	}
}

/** The deconstruction planner's special entity filters, by prototype name. */
const SPECIAL_FILTERS: Record<string, string> = {
	cliff: "Cliffs",
	"item-on-ground": "Items on ground",
	"item-request-proxy": "Item request slots",
	"entity-ghost": "Entity ghosts",
	"tile-ghost": "Tile ghosts",
	"deconstructible-tile-proxy": "Tile deconstruction",
};

/** Whether a filter is one of the deconstruction planner's special filters. */
export function isSpecialFilter(name: string): boolean {
	return name in SPECIAL_FILTERS;
}

/** The quality condition in words, e.g. "quality < rare"; empty for any quality. */
export function qualityCondition(filter: PlannerFilter): string {
	if (!filter.quality) return "";
	return `quality ${filter.comparator ?? "="} ${filter.quality}`;
}

/** A filter's tooltip: its name (or special filter label) and quality condition. */
export function filterLabel(filter: PlannerFilter): string {
	const parts = [SPECIAL_FILTERS[filter.name] ?? filter.name];
	const condition = qualityCondition(filter);
	if (condition) parts.push(condition);
	if (filter.module_limit) parts.push(`up to ${filter.module_limit} modules`);
	return parts.join(", ");
}

/** An upgrade target's tooltip: its name and the quality it becomes. */
export function targetLabel(filter: PlannerFilter): string {
	const parts = [filter.name];
	if (filter.quality && filter.quality !== "normal") parts.push(filter.quality);
	if (filter.module_limit) parts.push(`up to ${filter.module_limit} modules`);
	return parts.join(", ");
}

/** The icon kind of a planner filter: tiles, items (modules) or entities. */
export function filterIconType(filter: PlannerFilter, fallback: "entity" | "tile"): string {
	if (filter.type === "item" || filter.type === "entity" || filter.type === "tile") {
		return filter.type;
	}
	return fallback;
}

/** What a deconstruction planner removes, one line per setting. */
export function deconstructionSummary(settings: DeconstructionSettings): string[] {
	const lines: string[] = [];
	if (settings.tile_selection_mode === "only") {
		// Entity settings do not apply.
	} else if (settings.trees_and_rocks_only) {
		lines.push("Trees and rocks only");
	} else if (settings.entity_filters.length === 0) {
		lines.push("All entities");
	} else {
		lines.push(
			settings.entity_filter_mode === "blacklist"
				? "All entities except those listed"
				: "Only the listed entities",
		);
	}
	switch (settings.tile_selection_mode) {
		case "always":
			lines.push("Always removes tiles");
			break;
		case "never":
			lines.push("Never removes tiles");
			break;
		case "only":
			lines.push("Removes only tiles");
			break;
		default:
			lines.push("Removes tiles only when no entities are selected");
	}
	if (settings.tile_selection_mode !== "never" && settings.tile_filters.length > 0) {
		lines.push(
			settings.tile_filter_mode === "blacklist"
				? "All tiles except those listed"
				: "Only the listed tiles",
		);
	}
	return lines;
}
