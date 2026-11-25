import { describe, expect, it } from "vitest";

import { conditionGroups } from "./schedule";

describe("conditionGroups", () => {
	it("starts a group at every OR, as AND binds tighter in game", () => {
		const groups = conditionGroups([
			{ type: "full", compare_type: "or" },
			{ type: "inactivity", compare_type: "and" },
			{ type: "empty", compare_type: "or" },
			{ type: "time", compare_type: "and" },
			{ type: "fuel_full", compare_type: "or" },
		]);
		expect(groups.map((group) => group.map((condition) => condition.type))).toEqual([
			["full", "inactivity"],
			["empty", "time"],
			["fuel_full"],
		]);
	});

	it("ignores the first condition's compare type and treats a missing one as AND", () => {
		const groups = conditionGroups([{ type: "full", compare_type: "or" }, { type: "empty" }]);
		expect(groups).toHaveLength(1);
	});

	it("has no groups without conditions", () => {
		expect(conditionGroups(undefined)).toEqual([]);
		expect(conditionGroups([])).toEqual([]);
	});
});
