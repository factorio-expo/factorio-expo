// Pure helpers of the home page (pages/index.astro).
import type { BoardPlace, Challenge, Leaderboard, TrendingPeriod } from "./api";

export const TRENDING_PERIODS: { value: TrendingPeriod; label: string }[] = [
	{ value: "day", label: "Today" },
	{ value: "week", label: "This week" },
	{ value: "month", label: "This month" },
];

/** Entries trending needs before a period stands on its own as the default. */
export const MIN_TRENDING = 4;

/**
 * The tab trending opens on: this week, or this month while the week is still
 * thin (early on Monday). `counts` is how many entries each period has.
 */
export function defaultPeriod(counts: Record<TrendingPeriod, number>): TrendingPeriod {
	return counts.week < MIN_TRENDING && counts.month > counts.week ? "month" : "week";
}

/**
 * The challenge to feature: the open one that ends first, else the one in
 * voting that ends first. Upcoming, closed and draft challenges are left to
 * the challenges page.
 */
export function spotlightChallenge(challenges: Challenge[]): Challenge | undefined {
	const soonest = (status: Challenge["status"], endsAt: (c: Challenge) => string) =>
		challenges
			.filter((challenge) => challenge.status === status)
			.toSorted((a, b) => endsAt(a).localeCompare(endsAt(b)))[0];
	return (
		soonest("open", (challenge) => challenge.ends_at) ??
		soonest("judging", (challenge) => challenge.voting_ends_at)
	);
}

/** How many records the home page shows: the freshest, ships and factories alike. */
export const HOME_RECORDS = 4;

/**
 * The home page's fresh records: each shown board's leader (both groups), the
 * newest record first (one the minute task has not dated yet counts as
 * newest), one card per blueprint (its freshest record; the card's chip counts
 * the others), at most HOME_RECORDS.
 */
export function freshRecords(boards: Leaderboard[]): { board: Leaderboard; place: BoardPlace }[] {
	return boards
		.filter((board) => board.visible && board.places.length > 0)
		.map((board) => ({ board, place: board.places[0] }))
		.toSorted((a, b) =>
			(b.board.leader_since ?? "￿").localeCompare(a.board.leader_since ?? "￿"),
		)
		.filter((record, index, records) => {
			const leaf = (r: typeof record) => r.place.card.node_id ?? r.place.card.entry_id;
			return records.findIndex((other) => leaf(other) === leaf(record)) === index;
		})
		.slice(0, HOME_RECORDS);
}
