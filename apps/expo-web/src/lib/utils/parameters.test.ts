import { describe, expect, it } from "vitest";

import type { BlueprintParameter } from "@lib/api";
import { parameterRows } from "./parameters";

const asParameters = (value: unknown) => value as BlueprintParameter[];

describe("parameterRows", () => {
	it("returns no rows for missing input", () => {
		expect(parameterRows(null)).toEqual([]);
		expect(parameterRows(undefined)).toEqual([]);
	});

	it("maps a signal parameter with a relation as dependent", () => {
		const [row] = parameterRows(
			asParameters([
				{ type: "id", name: "Plate", id: "parameter-1", "ingredient-of": "parameter-0" },
			]),
		);
		expect(row).toMatchObject({
			kind: "id",
			slot: "parameter-1",
			relation: { label: "ingredient of", target: "parameter-0" },
			dependent: true,
		});
	});

	it("maps number parameters; a formula makes them dependent", () => {
		const [row] = parameterRows(
			asParameters([{ type: "number", number: "3", formula: "a*2" }]),
		);
		expect(row).toMatchObject({ kind: "number", value: "3", formula: "a*2", dependent: true });
	});

	it("keeps unknown parameters as raw JSON", () => {
		const [row] = parameterRows(asParameters([{ type: "mystery" }]));
		expect(row).toEqual({ kind: "unknown", raw: '{"type":"mystery"}', dependent: false });
	});
});
