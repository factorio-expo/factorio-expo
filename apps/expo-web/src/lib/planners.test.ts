import { describe, expect, it } from "vitest";
import type { DeconstructionSettings } from "@lib/api";
import {
	deconstructionSummary,
	filterLabel,
	isPlannerKind,
	qualityCondition,
	targetLabel,
} from "./planners";

const settings = (fields: Partial<DeconstructionSettings>): DeconstructionSettings => ({
	entity_filter_mode: "whitelist",
	entity_filters: [],
	trees_and_rocks_only: false,
	tile_filter_mode: "whitelist",
	tile_filters: [],
	tile_selection_mode: "normal",
	...fields,
});

describe("planner labels", () => {
	it("tells planner kinds apart", () => {
		expect(isPlannerKind("upgrade_planner")).toBe(true);
		expect(isPlannerKind("blueprint")).toBe(false);
	});

	it("labels special filters and quality conditions", () => {
		expect(filterLabel({ index: 0, name: "cliff" })).toBe("Cliffs");
		expect(qualityCondition({ index: 0, name: "inserter" })).toBe("");
		expect(filterLabel({ index: 0, name: "inserter", quality: "rare", comparator: "<" })).toBe(
			"inserter, quality < rare",
		);
		expect(
			targetLabel({ index: 0, name: "speed-module", quality: "epic", module_limit: 2 }),
		).toBe("speed-module, epic, up to 2 modules");
		expect(targetLabel({ index: 0, name: "iron-chest" })).toBe("iron-chest");
	});
});

describe("deconstructionSummary", () => {
	it("describes an empty planner", () => {
		expect(deconstructionSummary(settings({}))).toEqual([
			"All entities",
			"Removes tiles only when no entities are selected",
		]);
	});

	it("describes filter modes and tile filters", () => {
		expect(
			deconstructionSummary(
				settings({
					entity_filter_mode: "blacklist",
					entity_filters: [{ index: 0, name: "roboport" }],
					tile_selection_mode: "always",
					tile_filters: [{ index: 0, name: "stone-path" }],
				}),
			),
		).toEqual([
			"All entities except those listed",
			"Always removes tiles",
			"Only the listed tiles",
		]);
	});

	it("leaves out entity settings when it removes only tiles", () => {
		expect(
			deconstructionSummary(
				settings({
					entity_filters: [{ index: 0, name: "roboport" }],
					tile_selection_mode: "only",
				}),
			),
		).toEqual(["Removes only tiles"]);
	});

	it("puts trees and rocks only first and drops tile filters that never apply", () => {
		expect(
			deconstructionSummary(
				settings({
					trees_and_rocks_only: true,
					tile_selection_mode: "never",
					tile_filters: [{ index: 0, name: "stone-path" }],
				}),
			),
		).toEqual(["Trees and rocks only", "Never removes tiles"]);
	});
});
