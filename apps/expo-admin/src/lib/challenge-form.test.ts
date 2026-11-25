import { describe, expect, it } from "vitest";

import {
	challengeFieldsFromForm,
	editorValues,
	fromEditorTime,
	toEditorTime,
} from "./challenge-form";
import { ruleParams } from "./challenge-rules";

const form = (fields: Record<string, string>) => {
	const data = new FormData();
	for (const [name, value] of Object.entries(fields)) data.set(name, value);
	return data;
};

const rulesJson = JSON.stringify({
	goal: {
		param: "rate_out",
		target: { type: "item", name: "electronic-circuit", quality: "rare" },
		direction: "max",
	},
	restrictions: [{ param: "entity_count", target: { name: "beacon" }, op: "=", value: 0 }],
});

const base = {
	slug: "rare-circuits",
	title: "Rare circuits",
	starts_at: "2026-11-20 18:00",
	ends_at: "2026-12-04 18:00",
	voting_ends_at: "",
	votes_per_user: "3",
	content: "vanilla",
	kind: "factory",
	rules_json: rulesJson,
	voters_before_start: "on",
	hidden_until_start: "on",
};

const importsOf = (imports_json: string) =>
	challengeFieldsFromForm(form({ ...base, imports_json })).rules?.imports;

describe("challenge editor form", () => {
	it("reads times as UTC and writes them back the same way", () => {
		expect(fromEditorTime("2026-11-20 18:00")).toBe("2026-11-20T18:00:00.000Z");
		expect(fromEditorTime("20/11/2026")).toBe("");
		expect(toEditorTime("2026-11-20T18:00:00Z")).toBe("2026-11-20 18:00");
	});

	it("reads the whole form with the rules island's goal and restrictions", () => {
		const fields = challengeFieldsFromForm(form(base));
		expect(fields.rules).toEqual({
			content: "vanilla",
			kind: "factory",
			...JSON.parse(rulesJson),
		});
		expect(fields.voting_ends_at).toBe("2026-12-07T18:00:00.000Z");
		expect(fields.voters_before_start).toBe(true);
		expect(fields.hidden_until_start).toBe(true);
		expect(fields.require_new_revisions).toBe(false);
	});

	it("reads the allowed imports, and none when imports are not limited", () => {
		const imports = { allowed: [{ type: "item", name: "iron-plate" }] };
		expect(importsOf(JSON.stringify(imports))).toEqual(imports);
		expect(importsOf("")).toBeUndefined();
		expect(importsOf('{"allowed":[]}')).toEqual({ allowed: [] });
		expect(() => importsOf("nope")).toThrow(/imports/);
	});

	it("refuses missing times and unreadable rules", () => {
		expect(() => challengeFieldsFromForm(form({ ...base, starts_at: "" }))).toThrow(/start/);
		expect(() => challengeFieldsFromForm(form({ ...base, rules_json: "{" }))).toThrow(/goal/);
	});

	it("starts a new challenge a week out with no restrictions", () => {
		const values = editorValues();
		expect(values.rules?.restrictions).toEqual([]);
		expect(new Date(values.ends_at ?? "").getTime()).toBeGreaterThan(
			new Date(values.starts_at ?? "").getTime(),
		);
	});

	it("splits the parameters into item and numeric ones", () => {
		const groups = ruleParams([
			{ name: "rate_out", label: "", unit: "", target: "material", platform: false },
			{ name: "footprint_area", label: "", unit: "", target: "none", platform: false },
			{ name: "entity_count", label: "", unit: "", target: "entity", platform: false },
			{ name: "recipe_count", label: "", unit: "", target: "recipe", platform: false },
		]);
		expect(groups.itemParams.map((p) => p.name)).toEqual(["rate_out"]);
		expect(groups.numericParams.map((p) => p.name)).toEqual(["footprint_area", "entity_count"]);
	});
});
