// Pure helpers of the leaderboard (/leaderboard, the home page's records, the
// record chips on cards and entry pages) and of XP, levels and badges (a
// profile's ProfileProgress, the about page's table from GET /api/v1/xp-rules).
import type { Badge, Board, BoardGroup, BoardRecord, UserProgress, XPReason, XPRules } from "./api";

/** The leaderboard's groups, in the order its switch shows them. */
export const BOARD_GROUPS: { value: BoardGroup; label: string }[] = [
	{ value: "ships", label: "Ships" },
	{ value: "factories", label: "Factories" },
];

/** The group a `?group=` value names; ships for anything else. */
export function boardGroupOf(value: string | null | undefined): BoardGroup {
	return BOARD_GROUPS.find((group) => group.value === value)?.value ?? "ships";
}

/** A board's place on the leaderboard page: its group's tab, scrolled to the board. */
export function boardHref(group: BoardGroup, board: Board): string {
	const query = group === "ships" ? "" : `?group=${group}`;
	return `/leaderboard${query}#board-${board}`;
}

/** A record chip's text: "#1 Fastest ship". */
export function recordLabel(record: Pick<BoardRecord, "title">): string {
	return `#1 ${record.title}`;
}

/** What each badge is called on a profile and on the about page. */
export const BADGES: Record<Badge, string> = {
	first_upload: "First upload",
	ten_uploads: "10 uploads",
	first_favorite: "First favorite",
	favorites_25: "25 favorites",
	ten_comments: "10 comments",
	book_author: "Book author",
	record_holder: "Record holder",
	challenge_entrant: "Challenger",
	challenge_podium: "Podium",
	challenge_winner: "Challenge winner",
	first_challenge: "Challenge #1",
};

/** What earns XP, as the about page's table names it. */
export const XP_REASONS: Record<XPReason, string> = {
	upload: "Upload a public entry",
	favorite: "Someone favorites your entry",
	collection: "Someone adds your entry to a collection",
	comment: "Post a comment",
	record: "Take first place on a leaderboard board",
	challenge_win: "Win a design challenge",
	challenge_second: "Place second in a design challenge",
	challenge_third: "Place third in a design challenge",
	challenge_participation: "Take part in a design challenge",
};

/** One row of the about page's tables. */
export interface RuleRow {
	key: string;
	label: string;
	value: string;
	note: string;
}

/**
 * The about page's XP, level and badge tables, all from the API's rules (the
 * constants the code awards with), so the page never drifts from the code.
 */
export function xpRuleTables(rules: XPRules): {
	awards: RuleRow[];
	levels: RuleRow[];
	badges: RuleRow[];
} {
	return {
		awards: rules.awards.map((award) => ({
			key: award.reason,
			label: XP_REASONS[award.reason],
			value: `+${award.points.toLocaleString("en-US")}`,
			note: award.daily_cap ? `at most ${award.daily_cap} a day` : "",
		})),
		levels: rules.levels.map((level) => ({
			key: String(level.level),
			label: `Level ${level.level}`,
			value: `${level.xp.toLocaleString("en-US")} XP`,
			note: "",
		})),
		badges: rules.badges.map((badge) => ({
			key: badge.badge,
			label: BADGES[badge.badge],
			value: "",
			note: badge.criteria,
		})),
	};
}

/** How far a profile is through its level, 0-100; 100 at the top level. */
export function levelPercent(progress: UserProgress): number {
	const next = progress.next_level_xp;
	if (next === undefined) {
		return 100;
	}
	const span = next - progress.level_xp;
	return Math.max(0, Math.min(100, Math.floor(((progress.xp - progress.level_xp) / span) * 100)));
}

/** The XP still needed for the next level; undefined at the top level. */
export function xpToNextLevel(progress: UserProgress): number | undefined {
	return progress.next_level_xp === undefined ? undefined : progress.next_level_xp - progress.xp;
}

/** A record value with its unit: one decimal from 100 up, 3 significant digits below. */
export function formatRecordValue(value: number, unit: string): string {
	const shown =
		Math.abs(value) >= 100
			? value.toLocaleString("en-US", { maximumFractionDigits: 1 })
			: value.toLocaleString("en-US", { maximumSignificantDigits: 3 });
	return `${shown} ${unit}`;
}

const DAY_MS = 24 * 60 * 60 * 1000;

/** How long a record has been held: "since today", "for 1 day", "for 12 days". */
export function heldFor(since: string, now: Date = new Date()): string {
	const days = Math.floor((now.getTime() - Date.parse(since)) / DAY_MS);
	if (days < 1) {
		return "since today";
	}
	return days === 1 ? "for 1 day" : `for ${days.toLocaleString("en-US")} days`;
}
