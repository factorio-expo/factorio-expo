// Ban forms shared by the Bans and user pages: the choices, and the posts
// that create, change and revoke a ban.
import { FormError } from "./api/actions";
import type { AdminApi } from "./api/client";
import type { BanAction, BanReason, EntryReport } from "./api/types";
import { daysFromNow, formInt, formText } from "./forms";

export const MAX_BAN_DAYS = 3650;
export const banReasons: BanReason[] = ["spam", "abuse", "illegal_content", "other"];

export const banActions: BanAction[] = ["comment", "report", "upload"];

export const banActionLabel: Record<BanAction, string> = {
	comment: "commenting",
	report: "reporting",
	upload: "uploading",
};

export const reasonLabel = (code: string) => code.replaceAll("_", " ");

export const isBanReason = (value: string): value is BanReason =>
	banReasons.some((reason) => reason === value);

/**
 * What the "ban reporter" forms post: how long and why, plus the staff note.
 * Throws the FormError the page shows for a missing or invalid field.
 */
export function banReporterInput(form: FormData): {
	reason_code: BanReason;
	days: number;
	reason: string;
} {
	const days = formInt(form, "days", 1, MAX_BAN_DAYS);
	if (days === undefined) throw new FormError("Give the ban a length in days.");
	const code = formText(form, "reason_code");
	if (!isBanReason(code)) throw new FormError("Pick a reason.");
	return { reason_code: code, days, reason: formText(form, "reason") };
}

/** How staff read an entry report's reason. */
export const entryReportReasonText: Record<EntryReport["reason"], string> = {
	stolen: "Someone else's work",
	offensive: "Offensive",
	spam: "Spam",
	misleading: "Misleading",
	other: "Other",
};

/** Who sent a report: their account name, or what we know of an unnamed visitor. */
export const reporterLabel = (reporter: { anonymous: boolean; username?: string }) =>
	reporter.anonymous ? "anonymous visitor" : (reporter.username ?? "signed-in member");

/** The end a form asks for: permanent, or days from now. */
function formEnd(form: FormData): { expires_at?: string; permanent: boolean } {
	if (form.get("permanent") === "yes") return { permanent: true };
	const days = formInt(form, "days", 1, MAX_BAN_DAYS);
	if (days === undefined) {
		throw new FormError(`Give the ban a length of 1 to ${MAX_BAN_DAYS} days.`);
	}
	return { expires_at: daysFromNow(days), permanent: false };
}

/**
 * Handles the ban posts (action = ban-create, ban-end or ban-revoke) and
 * returns the notice; undefined for any other action.
 */
export async function handleBanPost(api: AdminApi, form: FormData): Promise<string | undefined> {
	switch (formText(form, "action")) {
		case "ban-create": {
			const username = formText(form, "username");
			if (!username) throw new FormError("Name the user to ban.");
			const actions = banActions.filter((action) => form.getAll("actions").includes(action));
			if (actions.length === 0) throw new FormError("Pick at least one thing to ban.");
			const reason = formText(form, "reason_code");
			if (!isBanReason(reason)) throw new FormError("Pick a reason.");
			await api.createBan({
				username,
				actions,
				reason_code: reason,
				note: formText(form, "note"),
				...formEnd(form),
			});
			return `${username} is banned from ${actions.map((action) => banActionLabel[action]).join(", ")}.`;
		}
		case "ban-end":
			await api.changeBanEnd(formText(form, "ban_id"), {
				...formEnd(form),
				reason: formText(form, "reason"),
			});
			return "Ban end changed.";
		case "ban-revoke":
			await api.revokeBan(formText(form, "ban_id"), formText(form, "reason"));
			return "Ban revoked.";
		default:
			return undefined;
	}
}
