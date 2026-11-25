// The import rule's editor <-> the staff API's ChallengeImportRule. The picker
// (a multi-select of items and fluids) keeps bare names; the rule needs each
// one's type, which the picker's own item and fluid lists give.
import { iconPicker } from "@factorio-expo/ui/game/data/icon-picker.data.ts";
import type { ChallengeImportRule, ChallengeTarget } from "./api/types";

/** Whole sets of imports that start most challenges, one click each. */
export const IMPORT_PRESETS: { label: string; names: string[] }[] = [
	{ label: "Iron and copper plates", names: ["iron-plate", "copper-plate"] },
	{ label: "Coal, water and crude oil", names: ["coal", "water", "crude-oil"] },
];

const FLUIDS: ReadonlySet<string> = new Set(
	iconPicker.fluid.flatMap((group) =>
		group.rows.flat().flatMap((token) => /^\[fluid=([^,\]]+)/.exec(token)?.[1] ?? []),
	),
);

/** The rule for the picked names: a fluid has no quality, an item any quality. */
export function importRule(names: string[]): ChallengeImportRule {
	return {
		allowed: names.map((name): ChallengeTarget => ({
			type: FLUIDS.has(name) ? "fluid" : "item",
			name,
		})),
	};
}

/** The picked names of a stored rule, in its order, each once. */
export function importNames(rule: ChallengeImportRule | undefined): string[] {
	return [...new Set((rule?.allowed ?? []).flatMap((target) => target.name ?? []))];
}

/** The names with a preset's added, keeping the order and dropping repeats. */
export function withPreset(names: string[], preset: string[]): string[] {
	return [...new Set([...names, ...preset])];
}
