// The challenge editor's form <-> the staff API's ChallengeFields. Times are
// UTC, the console's one time zone, written "2026-11-20 18:00" (the date
// picker's format). The goal and restrictions come from the rules island
// (components/ChallengeRules.svelte) as JSON.
import type {
	ChallengeFields,
	ChallengeImportRule,
	ChallengeRules,
	StaffChallenge,
} from "./api/types";
import { formInt, formText } from "./forms";

/** "2026-11-20 18:00" (UTC) from an API time. */
export function toEditorTime(iso: string): string {
	return new Date(iso).toISOString().slice(0, 16).replace("T", " ");
}

/** An API time from "2026-11-20 18:00" read as UTC; "" when empty or invalid. */
export function fromEditorTime(value: string): string {
	const match = /^(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2})$/.exec(value.trim());
	if (!match) return "";
	const date = new Date(`${match[1]}T${match[2]}:00Z`);
	return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}

/** The rules island's JSON: the goal and restrictions; throws when unreadable. */
function goalAndRestrictions(text: string): Pick<ChallengeRules, "goal" | "restrictions"> {
	let parsed: unknown;
	try {
		parsed = JSON.parse(text);
	} catch {
		throw new Error("The goal and restrictions could not be read.");
	}
	if (typeof parsed !== "object" || parsed === null || !("goal" in parsed))
		throw new Error("The goal and restrictions could not be read.");
	// The island writes this shape; the API validates every field.
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion
	const rules = parsed as Pick<ChallengeRules, "goal" | "restrictions">;
	return { goal: rules.goal, restrictions: rules.restrictions ?? [] };
}

/** The imports island's JSON; undefined when imports are not limited. */
function importsFromForm(text: string): ChallengeImportRule | undefined {
	if (!text) return undefined;
	let parsed: unknown;
	try {
		parsed = JSON.parse(text);
	} catch {
		throw new Error("The allowed imports could not be read.");
	}
	if (typeof parsed !== "object" || parsed === null || !("allowed" in parsed))
		throw new Error("The allowed imports could not be read.");
	// The island writes this shape; the API validates every field.
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion
	return parsed as ChallengeImportRule;
}

/** Reads the editor's form; throws an Error whose message is for staff. */
export function challengeFieldsFromForm(form: FormData): ChallengeFields {
	const content = formText(form, "content");
	const kind = formText(form, "kind");
	const rules: ChallengeRules = {
		content: content === "vanilla" ? "vanilla" : "space-age",
		kind: kind === "factory" || kind === "platform" ? kind : "any",
		...goalAndRestrictions(formText(form, "rules_json")),
	};
	const imports = importsFromForm(formText(form, "imports_json"));
	if (imports) rules.imports = imports;
	const times = {
		starts_at: fromEditorTime(formText(form, "starts_at")),
		ends_at: fromEditorTime(formText(form, "ends_at")),
		voting_ends_at: fromEditorTime(formText(form, "voting_ends_at")),
	};
	if (!times.starts_at || !times.ends_at)
		throw new Error("Give the start and the deadline as 2026-11-20 18:00 (UTC).");
	if (!times.voting_ends_at) {
		// Default: voting ends three days after the deadline.
		times.voting_ends_at = new Date(
			new Date(times.ends_at).getTime() + 3 * 24 * 60 * 60 * 1000,
		).toISOString();
	}
	const votes = formInt(form, "votes_per_user", 1, 20);
	if (votes === undefined) throw new Error("Votes per user are 1 to 20.");
	return {
		slug: formText(form, "slug"),
		title: formText(form, "title"),
		summary: formText(form, "summary"),
		description: formText(form, "description"),
		icons: formText(form, "icons"),
		rules,
		...times,
		require_new_revisions: form.has("require_new_revisions"),
		voters_before_start: form.has("voters_before_start"),
		voting_enabled: form.has("voting_enabled"),
		votes_per_user: votes,
		live_leaderboard: form.has("live_leaderboard"),
		hidden_until_start: form.has("hidden_until_start"),
		confirm_rescore: form.has("confirm_rescore"),
	};
}

/** The editor's starting values: the challenge, or a new one's defaults. */
export function editorValues(challenge?: StaffChallenge): Omit<ChallengeFields, "confirm_rescore"> {
	if (challenge) return challenge;
	const start = new Date();
	start.setUTCHours(18, 0, 0, 0);
	start.setUTCDate(start.getUTCDate() + 7);
	const day = 24 * 60 * 60 * 1000;
	return {
		slug: "",
		title: "",
		summary: "",
		description: "",
		icons: "",
		rules: {
			content: "space-age",
			kind: "any",
			goal: { param: "footprint_area", target: {}, direction: "min" },
			restrictions: [],
		},
		starts_at: start.toISOString(),
		ends_at: new Date(start.getTime() + 14 * day).toISOString(),
		voting_ends_at: new Date(start.getTime() + 17 * day).toISOString(),
		require_new_revisions: true,
		voters_before_start: true,
		voting_enabled: true,
		votes_per_user: 3,
		live_leaderboard: true,
		hidden_until_start: false,
	};
}

/**
 * Runs the editor's save. On failure the page renders the editor again with
 * what was typed (when the form could be read) and the error, so nothing is lost.
 */
export async function saveEditor(
	form: FormData,
	save: (fields: ChallengeFields) => Promise<void>,
	failureMessage: (error: unknown) => string,
): Promise<{ error?: string; draft?: ChallengeFields }> {
	let fields: ChallengeFields;
	try {
		fields = challengeFieldsFromForm(form);
	} catch (error) {
		return { error: error instanceof Error ? error.message : "The form is not valid." };
	}
	try {
		await save(fields);
		return {};
	} catch (error) {
		return { error: failureMessage(error), draft: fields };
	}
}
