import { describe, expect, it } from "vitest";
import type { Badge, UserProgress, XPReason, XPRules } from "@lib/api";
import {
	BADGES,
	XP_REASONS,
	boardGroupOf,
	boardHref,
	formatRecordValue,
	heldFor,
	levelPercent,
	recordLabel,
	xpRuleTables,
	xpToNextLevel,
} from "./achievements";

function progress(overrides: Partial<UserProgress>): UserProgress {
	return {
		xp: 0,
		level: 1,
		level_xp: 0,
		next_level_xp: 50,
		badges: [],
		records: [],
		...overrides,
	};
}

describe("levelPercent and xpToNextLevel", () => {
	it("measure the way through the current level", () => {
		const middle = progress({ xp: 100, level: 2, level_xp: 50, next_level_xp: 150 });
		expect(levelPercent(middle)).toBe(50);
		expect(xpToNextLevel(middle)).toBe(50);
		expect(levelPercent(progress({}))).toBe(0);
	});

	it("show a full bar and nothing left at the top level", () => {
		const top = progress({ xp: 7000, level: 10, level_xp: 5000, next_level_xp: undefined });
		expect(levelPercent(top)).toBe(100);
		expect(xpToNextLevel(top)).toBeUndefined();
	});
});

describe("formatRecordValue", () => {
	it("rounds to one decimal from 100 up and 3 significant digits below", () => {
		expect(formatRecordValue(12, "cargo bays")).toBe("12 cargo bays");
		expect(formatRecordValue(1200.54, "t")).toBe("1,200.5 t");
		expect(formatRecordValue(230.25, "km/s")).toBe("230.3 km/s");
		expect(formatRecordValue(0.012345, "MN/t")).toBe("0.0123 MN/t");
	});
});

describe("heldFor", () => {
	const now = new Date("2026-11-16T12:00:00Z");
	it("counts whole days", () => {
		expect(heldFor("2026-11-16T01:00:00Z", now)).toBe("since today");
		expect(heldFor("2026-11-15T11:00:00Z", now)).toBe("for 1 day");
		expect(heldFor("2026-11-01T12:00:00Z", now)).toBe("for 15 days");
	});
});

describe("boards", () => {
	it("link a board on its group's tab and name a record", () => {
		expect(boardHref("ships", "fastest")).toBe("/leaderboard#board-fastest");
		expect(boardHref("factories", "most_science")).toBe(
			"/leaderboard?group=factories#board-most_science",
		);
		expect(boardGroupOf("factories")).toBe("factories");
		expect(boardGroupOf("planets")).toBe("ships");
		expect(boardGroupOf(null)).toBe("ships");
		expect(recordLabel({ title: "Fastest ship" })).toBe("#1 Fastest ship");
	});
});

describe("xpRuleTables (the about page)", () => {
	// The rules as GET /api/v1/xp-rules returns them: every badge and reason
	// the API knows (the Record types make the label maps cover them all).
	const rules: XPRules = {
		awards: (Object.keys(XP_REASONS) as XPReason[]).map((reason, index) => ({
			reason,
			points: index + 1,
			daily_cap: reason === "upload" ? 5 : undefined,
		})),
		levels: [0, 50, 150, 300, 500, 800, 1200, 2000, 3000, 5000].map((xp, index) => ({
			level: index + 1,
			xp,
		})),
		badges: (Object.keys(BADGES) as Badge[]).map((badge) => ({
			badge,
			criteria: `Earn ${badge}.`,
		})),
		daily_cap_reset: "00:00 UTC",
	};

	it("lists every badge with its label and criteria", () => {
		const { badges } = xpRuleTables(rules);
		expect(badges.map((row) => row.key)).toEqual(Object.keys(BADGES));
		for (const row of badges) {
			expect(row.label).toBe(BADGES[row.key as Badge]);
			expect(row.note).toBe(`Earn ${row.key}.`);
		}
	});

	it("lists every level and every award with its cap", () => {
		const { awards, levels } = xpRuleTables(rules);
		expect(levels).toHaveLength(10);
		expect(levels[9]).toMatchObject({ label: "Level 10", value: "5,000 XP" });
		expect(awards.map((row) => row.key)).toEqual(Object.keys(XP_REASONS));
		expect(awards[0]).toMatchObject({ value: "+1", note: "at most 5 a day" });
		expect(awards[1]?.note).toBe("");
	});
});
