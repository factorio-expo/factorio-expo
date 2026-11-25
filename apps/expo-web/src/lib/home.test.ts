import { describe, expect, it } from "vitest";
import type { BoardPlace, Challenge, Leaderboard } from "./api";
import { defaultPeriod, freshRecords, HOME_RECORDS, spotlightChallenge } from "./home";

const challenge = (slug: string, status: Challenge["status"], ends: string, voting = ends) =>
	({ slug, status, ends_at: ends, voting_ends_at: voting }) as Challenge;

describe("defaultPeriod", () => {
	it("opens on the week unless it is thin and the month is fuller", () => {
		expect(defaultPeriod({ day: 0, week: 6, month: 8 })).toBe("week");
		expect(defaultPeriod({ day: 0, week: 2, month: 8 })).toBe("month");
		expect(defaultPeriod({ day: 0, week: 0, month: 0 })).toBe("week");
	});
});

describe("spotlightChallenge", () => {
	it("prefers the open challenge ending first", () => {
		const picked = spotlightChallenge([
			challenge("voting", "judging", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"),
			challenge("late", "open", "2026-12-01T00:00:00Z"),
			challenge("soon", "open", "2026-11-20T00:00:00Z"),
		]);
		expect(picked?.slug).toBe("soon");
	});

	it("falls back to voting, and ignores the rest", () => {
		expect(
			spotlightChallenge([
				challenge("old", "closed", "2026-01-01T00:00:00Z"),
				challenge("next", "upcoming", "2027-01-01T00:00:00Z"),
				challenge("voting", "judging", "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"),
			])?.slug,
		).toBe("voting");
		expect(
			spotlightChallenge([challenge("old", "closed", "2026-01-01T00:00:00Z")]),
		).toBeUndefined();
	});
});

const board = (name: Leaderboard["board"], since?: string, visible = true): Leaderboard => ({
	board: name,
	group: name.includes("science") ? "factories" : "ships",
	title: name,
	description: "",
	unit: "t",
	lower_wins: false,
	min_entries: 10,
	eligible: visible ? 10 : 3,
	visible,
	leader_since: since,
	places: visible ? [{ rank: 1, value: 1, card: { entry_id: name } as BoardPlace["card"] }] : [],
});

describe("freshRecords", () => {
	it("lists the shown boards' leaders of both groups, newest record first", () => {
		const records = freshRecords([
			board("fastest", "2026-11-01T00:00:00Z"),
			board("lightest", undefined, false),
			board("most_cargo", "2026-11-10T00:00:00Z"),
			board("thrust_to_weight"),
			board("most_science", "2026-11-12T00:00:00Z"),
		]);
		expect(records.map((record) => record.board.board)).toEqual([
			"thrust_to_weight",
			"most_science",
			"most_cargo",
			"fastest",
		]);
		expect(records[0]?.place.card.entry_id).toBe("thrust_to_weight");
	});

	it("shows a blueprint that leads several boards once, with its freshest record", () => {
		const shared = (name: Leaderboard["board"], since: string) => {
			const view = board(name, since);
			view.places[0].card = { entry_id: "same", node_id: "leaf" } as BoardPlace["card"];
			return view;
		};
		const records = freshRecords([
			shared("most_science", "2026-11-01T00:00:00Z"),
			shared("densest_science", "2026-11-03T00:00:00Z"),
			board("fastest", "2026-11-02T00:00:00Z"),
		]);
		expect(records.map((record) => record.board.board)).toEqual(["densest_science", "fastest"]);
	});

	it("keeps the freshest few", () => {
		const records = freshRecords([
			board("fastest", "2026-11-01T00:00:00Z"),
			board("most_cargo", "2026-11-02T00:00:00Z"),
			board("most_science", "2026-11-03T00:00:00Z"),
			board("densest_science", "2026-11-04T00:00:00Z"),
			board("cleanest_science", "2026-11-05T00:00:00Z"),
		]);
		expect(records).toHaveLength(HOME_RECORDS);
		expect(records.map((record) => record.board.board)).not.toContain("fastest");
	});
});
