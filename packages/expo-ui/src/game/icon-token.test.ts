import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
	ICON_TYPES,
	type IconToken,
	QUALITIES,
	formatToken,
	parseToken,
	scanTokens,
	stripTokens,
} from "./icon-token";

const fixture = JSON.parse(
	readFileSync(new URL("../../../../test-data/icon-tokens.json", import.meta.url), "utf8"),
);

const byName = (a: string, b: string) => a.localeCompare(b);

describe("icon-token fixture", () => {
	it("agrees on the type and quality lists", () => {
		expect(Array.from(ICON_TYPES).toSorted(byName)).toEqual(fixture.iconTypes.toSorted(byName));
		expect([...QUALITIES]).toEqual(fixture.qualities);
	});
	it.each<{ token: string; want: unknown }>(fixture.parse)("parses $token", ({ token, want }) => {
		expect(parseToken(token) ?? null).toEqual(want);
	});
	it.each<{ token: IconToken; keepNormal: boolean; want: string }>(fixture.format)(
		"formats $want",
		({ token, keepNormal, want }) => {
			expect(formatToken(token, { keepNormal })).toBe(want);
		},
	);
	it.each<{ text: string; want: unknown }>(fixture.scan)("scans $text", ({ text, want }) => {
		expect(scanTokens(text)).toEqual(want);
	});
	it.each<{ text: string; want: string }>(fixture.strip)("strips $text", ({ text, want }) => {
		expect(stripTokens(text)).toBe(want);
	});
});
