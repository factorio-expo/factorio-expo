import { describe, expect, it } from "vitest";

import { IMPORT_PRESETS, importNames, importRule, withPreset } from "./challenge-imports";

describe("import rule editor", () => {
	it("types each picked name as an item or a fluid", () => {
		expect(importRule(["iron-plate", "water", "crude-oil", "coal"])).toEqual({
			allowed: [
				{ type: "item", name: "iron-plate" },
				{ type: "fluid", name: "water" },
				{ type: "fluid", name: "crude-oil" },
				{ type: "item", name: "coal" },
			],
		});
		expect(importRule([])).toEqual({ allowed: [] });
	});

	it("reads a stored rule back as names", () => {
		expect(importNames(undefined)).toEqual([]);
		expect(
			importNames({
				allowed: [
					{ type: "item", name: "iron-plate" },
					{ type: "item", name: "iron-plate", quality: "rare" },
					{ type: "fluid", name: "water" },
				],
			}),
		).toEqual(["iron-plate", "water"]);
	});

	it("adds a preset without repeating names", () => {
		expect(withPreset(["coal", "iron-plate"], IMPORT_PRESETS[0].names)).toEqual([
			"coal",
			"iron-plate",
			"copper-plate",
		]);
	});
});
