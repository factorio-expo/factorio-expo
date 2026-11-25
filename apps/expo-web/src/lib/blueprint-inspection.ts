// What the upload form makes of an inspect result (POST /api/v1/blueprints/inspect).
import type { BlueprintInspection } from "@lib/api";
import { stripRichText } from "@factorio-expo/ui/game/rich-text.ts";
import { modLabels } from "@lib/mods";
import { versionBadge } from "@lib/version-badge";
import { isPlannerKind, nodeKindLabel } from "@lib/planners";

/** Entry field bounds (Go catalog: title 160, description 5000). */
const MAX_TITLE = 160;
const MAX_DESCRIPTION = 5000;

const count = (n: number, one: string, many: string) =>
	`${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;

/** One line, e.g. `Book "Mall" · 12 blueprints · 3,410 entities · Factorio 2.0.15 · needs Space Age`. */
export function inspectionSummary(inspection: BlueprintInspection): string {
	const label = stripRichText(inspection.label).trim();
	const kind = inspection.kind === "book" ? "Book" : nodeKindLabel(inspection.kind);
	const parts = [label ? `${kind} "${label}"` : kind];
	if (inspection.kind === "book") {
		parts.push(count(inspection.blueprint_count, "blueprint", "blueprints"));
		if (inspection.planner_count > 0) {
			parts.push(count(inspection.planner_count, "planner", "planners"));
		}
	}
	if (!isPlannerKind(inspection.kind)) {
		parts.push(count(inspection.entity_count, "entity", "entities"));
	}
	if (inspection.game_version) parts.push(`Factorio ${inspection.game_version}`);
	const badge = versionBadge(inspection.game_version, inspection.compat_21);
	if (badge && badge.text !== "2.1") parts.push(badge.text);
	const mods = modLabels(inspection.mods);
	if (mods.length > 0) parts.push(`needs ${mods.map((mod) => mod.text).join(", ")}`);
	return parts.join(" · ");
}

/** The title and description the "copy" button fills; a blank field is left out. */
export function inspectionDetails(inspection: BlueprintInspection): {
	title?: string;
	description?: string;
} {
	const title = inspection.label.trim().slice(0, MAX_TITLE);
	const description = inspection.description.trim().slice(0, MAX_DESCRIPTION);
	return {
		...(title && { title }),
		...(description && { description }),
	};
}
