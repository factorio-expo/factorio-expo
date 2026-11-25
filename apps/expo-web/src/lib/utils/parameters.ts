// View model for BlueprintParameters: turns the blueprint's raw parameter
// objects into display rows. Read-only; formulas are shown, never evaluated.
import type { BlueprintParameter } from "@lib/api";

const relationships = [
	["ingredient-of", "ingredient of"],
	["item-ingredient-of", "item ingredient of"],
	["fluid-ingredient-of", "fluid ingredient of"],
	["product-of", "product of"],
	["item-product-of", "item product of"],
	["fluid-product-of", "fluid product of"],
] as const;

export const isSlot = (id: unknown): id is string =>
	typeof id === "string" && /^parameter-[0-9]$/.test(id);
const relationOf = (parameter: BlueprintParameter) => {
	const found = relationships.find(
		([key]) => typeof parameter[key] === "string" && parameter[key] !== "",
	);
	return found ? { label: found[1], target: text(parameter[found[0]]) } : undefined;
};
const text = (value: unknown): string => (typeof value === "string" ? value : "");

export function parameterRows(parameters: BlueprintParameter[] | null | undefined) {
	const rows = Array.isArray(parameters) ? parameters : [];
	return rows.map((parameter) => {
		if (parameter && parameter.type === "id") {
			const relation = relationOf(parameter);
			const quality = parameter["quality-condition"];
			return {
				kind: "id" as const,
				name: text(parameter.name),
				slot: isSlot(parameter.id) ? parameter.id : undefined,
				value: text(parameter.id),
				relation,
				quality: quality?.quality
					? { name: quality.quality, comparator: quality.comparator ?? "=" }
					: undefined,
				dependent: relation !== undefined,
			};
		}
		if (parameter && parameter.type === "number") {
			const number = parameter;
			return {
				kind: "number" as const,
				name: text(number.name),
				value: text(number.number),
				variable: text(number.variable),
				formula: text(number.formula),
				fixed: number["not-parametrised"] === true,
				dependent: number.dependent === true || text(number.formula) !== "",
			};
		}
		return {
			kind: "unknown" as const,
			raw: JSON.stringify(parameter) ?? "-",
			dependent: false,
		};
	});
}
