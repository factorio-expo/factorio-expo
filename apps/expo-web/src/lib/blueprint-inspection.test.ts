import { describe, expect, it } from "vitest";
import type { BlueprintInspection } from "@lib/api";
import { inspectionDetails, inspectionSummary } from "./blueprint-inspection";

const inspection = (fields: Partial<BlueprintInspection>): BlueprintInspection => ({
	kind: "blueprint",
	label: "",
	description: "",
	icons: "",
	game_version: "",
	blueprint_count: 1,
	planner_count: 0,
	entity_count: 0,
	entities: [],
	recipes: [],
	suggested_tags: [],
	mods: { modsets: [], unknown_entities: [], unknown_recipes: [] },
	compat_21: "",
	...fields,
});

describe("inspectionSummary", () => {
	it("shows 2.1 compatibility", () => {
		const text = inspectionSummary(
			inspection({
				game_version: "2.0.55",
				compat_21: "incompatible",
			}),
		);
		expect(text).toContain("2.1 incompatible");
	});

	it("names a book's blueprints and the game version", () => {
		expect(
			inspectionSummary(
				inspection({
					kind: "book",
					label: "[item=iron-plate] Mall",
					blueprint_count: 12,
					entity_count: 3410,
					game_version: "2.0.15",
				}),
			),
		).toBe('Book "Mall" · 12 blueprints · 3,410 entities · Factorio 2.0.15');
	});

	it("counts a book's planners and names a planner", () => {
		expect(
			inspectionSummary(
				inspection({ kind: "book", label: "Tools", blueprint_count: 1, planner_count: 3 }),
			),
		).toBe('Book "Tools" · 1 blueprint · 3 planners · 0 entities');
		expect(
			inspectionSummary(inspection({ kind: "deconstruction_planner", label: "Cliffs" })),
		).toBe('Deconstruction planner "Cliffs"');
	});

	it("names the mods it needs", () => {
		expect(
			inspectionSummary(
				inspection({
					entity_count: 2,
					mods: {
						modsets: [
							{ name: "space-age", title: "Space Age", builtin: true, score: 1 },
							{
								name: "aai-loaders",
								title: "AAI Loaders",
								builtin: false,
								score: 0.666,
							},
						],
						unknown_entities: ["mystery-machine"],
						unknown_recipes: [],
					},
				}),
			),
		).toBe("Blueprint · 2 entities · needs Space Age, AAI Loaders (67%), unknown mod");
	});

	it("leaves out what a blueprint does not have", () => {
		expect(inspectionSummary(inspection({ entity_count: 1 }))).toBe("Blueprint · 1 entity");
	});
});

describe("inspectionDetails", () => {
	it("keeps icon tokens and drops blank fields", () => {
		expect(inspectionDetails(inspection({ label: " [item=iron-plate] Mall " }))).toEqual({
			title: "[item=iron-plate] Mall",
		});
		expect(inspectionDetails(inspection({ description: "Notes" }))).toEqual({
			description: "Notes",
		});
	});

	it("fits the entry field bounds", () => {
		const details = inspectionDetails(
			inspection({ label: "x".repeat(200), description: "y".repeat(6000) }),
		);
		expect(details.title).toHaveLength(160);
		expect(details.description).toHaveLength(5000);
	});
});
