import { describe, expect, it } from "vitest";

import { LEGAL_PLACEHOLDERS, parseLegal, securityTxt } from "./legal";

describe("parseLegal", () => {
	it("shows placeholders when unset", () => {
		expect(parseLegal(undefined)).toEqual(LEGAL_PLACEHOLDERS);
		expect(parseLegal("  ")).toEqual(LEGAL_PLACEHOLDERS);
	});

	it("fills the given fields and keeps placeholders for the rest", () => {
		expect(
			parseLegal('{"name":" Jane Doe ","email":"a@example.org","emailProvider":""}'),
		).toEqual({
			...LEGAL_PLACEHOLDERS,
			name: "Jane Doe",
			email: "a@example.org",
		});
	});

	it("refuses malformed values", () => {
		expect(() => parseLegal("{name: 'x'}")).toThrow(/not valid JSON/);
		expect(() => parseLegal('["x"]')).toThrow(/JSON object/);
		expect(() => parseLegal('{"provider":"x"}')).toThrow(/unknown key "provider"/);
		expect(() => parseLegal('{"name":1}')).toThrow(/PUBLIC_LEGAL.name must be a string/);
	});
});

describe("securityTxt", () => {
	const text = securityTxt(
		new URL("https://example.org"),
		new Date("2026-11-06T12:00:00.500Z"),
		"security@example.org",
	);
	const fields = Object.fromEntries(
		text
			.trimEnd()
			.split("\n")
			.map((line) => line.split(/: (.*)/s).slice(0, 2)),
	);

	it("has the required fields of RFC 9116", () => {
		expect(fields.Contact).toBe("mailto:security@example.org");
		expect(fields.Expires).toBe("2027-11-01T12:00:00Z");
	});

	it("expires in under a year", () => {
		const days = (Date.parse(fields.Expires) - Date.parse("2026-11-06T12:00:00Z")) / 86_400_000;
		expect(days).toBeLessThan(365);
	});

	it("names its canonical URL and languages", () => {
		expect(fields.Canonical).toBe("https://example.org/.well-known/security.txt");
		expect(fields["Preferred-Languages"]).toBe("en");
	});
});
