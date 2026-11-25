// The challenge rules editor's parameter groups and operators.
import type { ChallengeParam } from "./api/types";

/** Restriction comparisons (challenge.Ops). */
export const OPERATORS = ["<", "<=", "=", ">=", ">"] as const;

/**
 * The catalogue split for the editor: parameters of an item or fluid (rates)
 * and plain numbers (footprint, power, build cost, platform, all entities).
 * Counts of one entity or recipe have their own rows.
 */
export function ruleParams(params: ChallengeParam[]): {
	itemParams: ChallengeParam[];
	numericParams: ChallengeParam[];
} {
	return {
		itemParams: params.filter((param) => param.target === "material"),
		numericParams: params.filter(
			(param) => param.target !== "material" && param.target !== "recipe",
		),
	};
}
