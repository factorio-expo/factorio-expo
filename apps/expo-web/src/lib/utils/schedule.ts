import type { ScheduleCondition, ScheduleSignal, ScheduleWaitCondition } from "@lib/api";

/** Icon token parts for a signal, mapping the blueprint's "virtual" to the icon table's type. */
export function signalIcon(signal: ScheduleSignal | undefined) {
	if (!signal?.name) return undefined;
	const type = signal.type === "virtual" ? "virtual-signal" : (signal.type ?? "item");
	return { type, name: signal.name, quality: signal.quality };
}

export function formatTicks(ticks: number | undefined): string {
	const seconds = (ticks ?? 0) / 60;
	if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60} min`;
	return `${Number.isInteger(seconds) ? seconds : seconds.toFixed(1)} s`;
}

const LABELS: Record<string, string> = {
	time: "Wait",
	inactivity: "Inactivity",
	full: "Cargo full",
	empty: "Cargo empty",
	not_empty: "Cargo not empty",
	fuel_full: "Fuel full",
	passenger_present: "Passenger present",
	passenger_not_present: "Passenger not present",
	circuit: "Circuit condition",
	item_count: "Item count",
	fluid_count: "Fluid count",
	fuel_item_count_all: "All fuel items",
	fuel_item_count_any: "Any fuel item",
	at_station: "At station",
	not_at_station: "Not at station",
	destination_full_or_no_path: "Destination full or no path",
	specific_destination_full: "Destination full",
	specific_destination_not_full: "Destination not full",
	all_requests_satisfied: "All requests satisfied",
	any_request_not_satisfied: "Any request not satisfied",
	any_request_zero: "Any request zero",
	request_satisfied: "Request satisfied",
	request_not_satisfied: "Request not satisfied",
	any_planet_import_zero: "Planet import zero",
	damage_taken: "Damage taken",
};

export interface ConditionView {
	label: string;
	/** Free-text detail such as a duration or station name. */
	detail?: string;
	/** Circuit-style comparison, rendered with signal icons. */
	comparison?: ScheduleCondition;
}

export function describeCondition(condition: ScheduleWaitCondition): ConditionView {
	const type = (condition.type ?? "").replaceAll("-", "_");
	const label = LABELS[type] ?? type.replaceAll("_", " ").replace(/^./, (c) => c.toUpperCase());
	switch (type) {
		case "time":
		case "inactivity":
			return { label, detail: formatTicks(condition.ticks) };
		case "damage_taken":
			return { label, detail: String(condition.damage ?? 0) };
		case "at_station":
		case "not_at_station":
		case "specific_destination_full":
		case "specific_destination_not_full":
			return { label, detail: condition.station };
		case "any_planet_import_zero":
			return { label, detail: condition.planet?.name };
		default:
			return condition.condition ? { label, comparison: condition.condition } : { label };
	}
}

/**
 * Wait or interrupt conditions grouped as the game evaluates them: AND binds
 * tighter than OR, so every condition joined with "or" starts a new group, and
 * the list holds when all conditions of any one group hold. The first
 * condition's own compare type is ignored, as in game.
 */
export function conditionGroups(
	conditions: ScheduleWaitCondition[] | undefined,
): ScheduleWaitCondition[][] {
	const groups: ScheduleWaitCondition[][] = [];
	for (const [index, condition] of (conditions ?? []).entries()) {
		if (index === 0 || condition.compare_type === "or") groups.push([condition]);
		else groups.at(-1)!.push(condition);
	}
	return groups;
}
