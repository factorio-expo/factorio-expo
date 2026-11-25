import { describe, expect, it } from "vitest";
import type { CompareLeaf } from "@lib/api";
import { MAX_ROWS, MISSING, compareGroups } from "./compare";

function leaf(
	overrides: Omit<Partial<CompareLeaf>, "production"> & {
		production?: Partial<CompareLeaf["production"]>;
	},
): CompareLeaf {
	const { production, ...rest } = overrides;
	return {
		id: "aaaaaaaaaaaa",
		entry_id: "eeeeeeeeeeee",
		entry_title: "Gears",
		label: "Gears",
		description: "",
		icons: "",
		game_version: "2.1.3",
		entity_count: 1234,
		mod_badges: [],
		spoiler: false,
		production: { availability: "ready", stale: false, ...production },
		...rest,
	};
}

const rate = (name: string, output?: number, input?: number) => ({
	type: "item" as const,
	name,
	quality: "normal",
	...(output === undefined ? {} : { output_per_second: output }),
	...(input === undefined ? {} : { input_per_second: input }),
});

const find = (groups: ReturnType<typeof compareGroups>, id: string) =>
	groups.find((group) => group.id === id)?.rows ?? [];

describe("compareGroups", () => {
	const a = leaf({
		production: {
			footprint: { width: 32, height: 16 },
			electricity: {
				consumption_watts: 2_500_000,
				production_watts: 0,
				net_watts: -2_500_000,
			},
			products: [rate("iron-gear-wheel", 7.5), rate("iron-plate", 1)],
			ingredients: [rate("iron-plate", undefined, 15)],
			build_cost: {
				items: [
					{ type: "item", name: "assembling-machine-2", quality: "normal", count: 20 },
				],
				raw: [{ type: "item", name: "iron-ore", amount: 1500 }],
			},
		},
	});
	const b = leaf({
		id: "bbbbbbbbbbbb",
		game_version: "2.0.55",
		entity_count: 80,
		production: {
			footprint: { width: 10, height: 10 },
			products: [rate("iron-gear-wheel", 2), rate("copper-cable", 4)],
		},
	});

	it("puts each metric's two sides in one row", () => {
		const groups = compareGroups(a, b);
		expect(find(groups, "overview").map((row) => [row.id, row.a, row.b])).toEqual([
			["entities", "1,234", "80"],
			["footprint", "32 × 16 tiles", "10 × 10 tiles"],
			["area", "512 tiles", "100 tiles"],
			["game-version", "2.1.3", "2.0.55"],
		]);
		expect(find(groups, "power").map((row) => [row.id, row.a, row.b])).toEqual([
			["consumption", "2,500 kW", MISSING],
			["production", "0 kW", MISSING],
			["net-power", "-2,500 kW", MISSING],
		]);
	});

	it("lists the union of products, the larger first, missing where a side lacks one", () => {
		const rows = find(compareGroups(a, b), "products");
		expect(rows.map((row) => [row.label, row.a, row.b])).toEqual([
			["iron-gear-wheel", "7.5/s", "2/s"],
			["copper-cable", MISSING, "4/s"],
			["iron-plate", "1/s", MISSING],
		]);
		expect(rows[0].iconToken).toBe("[item=iron-gear-wheel]");
		expect(
			find(compareGroups(a, b), "ingredients").map((row) => [row.label, row.a, row.b]),
		).toEqual([["iron-plate", "15/s", MISSING]]);
	});

	it("marks the larger figure of a numeric row, none for ties, text or a missing side", () => {
		const groups = compareGroups(a, b);
		const larger = (group: string) => find(groups, group).map((row) => [row.id, row.larger]);
		expect(larger("overview")).toEqual([
			["entities", "a"],
			["footprint", undefined],
			["area", "a"],
			["game-version", undefined],
		]);
		expect(larger("power")).toEqual([
			["consumption", undefined],
			["production", undefined],
			["net-power", undefined],
		]);
		expect(larger("products")).toEqual([
			["products-item:iron-gear-wheel:normal", "a"],
			["products-item:copper-cable:normal", undefined],
			["products-item:iron-plate:normal", undefined],
		]);
		expect(find(compareGroups(b, a), "overview")[0].larger).toBe("b");
		expect(find(compareGroups(a, a), "overview")[0].larger).toBeUndefined();
	});

	it("shows the build cost", () => {
		const groups = compareGroups(a, b);
		expect(find(groups, "cost")[0]).toMatchObject({ a: "20", b: MISSING });
		expect(find(groups, "raw-cost")[0]).toMatchObject({
			label: "iron-ore",
			a: "1.5K",
			b: MISSING,
		});
	});

	it("shows the space platform group only when a side is one", () => {
		expect(compareGroups(a, b).some((group) => group.id === "platform")).toBe(false);
		const ship = leaf({
			production: {
				space_platform: {
					is_space_platform: true,
					weight_tons: 120,
					thrust_mn: 3.5,
					max_speed_km_s: 40,
				},
			},
		});
		expect(
			find(compareGroups(ship, b), "platform").map((row) => [row.id, row.a, row.b]),
		).toEqual([
			["weight", "120 t", MISSING],
			["thrust", "3.5 MN", MISSING],
			["speed", "40 km/s", MISSING],
		]);
	});

	it("shows nothing from an analysis that is not ready", () => {
		const pending = leaf({
			production: { availability: "pending", products: [rate("iron-plate", 9)] },
		});
		const groups = compareGroups(pending, b);
		expect(find(groups, "overview").find((row) => row.id === "footprint")).toMatchObject({
			a: MISSING,
			b: "10 × 10 tiles",
		});
		expect(find(groups, "products").map((row) => row.label)).toEqual([
			"copper-cable",
			"iron-gear-wheel",
		]);
	});

	it("caps a group at MAX_ROWS and says how many were left out", () => {
		const many = leaf({
			production: {
				products: Array.from({ length: MAX_ROWS + 3 }, (_, index) =>
					rate(`item-${index}`, index + 1),
				),
			},
		});
		const group = compareGroups(many, b).find((candidate) => candidate.id === "products")!;
		expect(group.rows).toHaveLength(MAX_ROWS);
		expect(group.omitted).toBeGreaterThanOrEqual(3);
	});
});
