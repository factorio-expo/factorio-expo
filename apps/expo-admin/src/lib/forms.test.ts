import { describe, expect, it } from "vitest";

import { formatBytes, toGiB } from "./format";
import { daysFromNow, formGiB, formInt, formText, outcomeUrl } from "./forms";

const form = (fields: Record<string, string>) => {
	const data = new FormData();
	for (const [name, value] of Object.entries(fields)) data.set(name, value);
	return data;
};

describe("form fields", () => {
	it("reads trimmed text, and nothing for a missing field", () => {
		expect(formText(form({ reason: "  spam  " }), "reason")).toBe("spam");
		expect(formText(form({}), "reason")).toBe("");
	});

	it("reads whole numbers within bounds only", () => {
		const fields = form({ ok: "7", big: "31", neg: "-1", frac: "1.5", word: "x" });
		expect(formInt(fields, "ok", 1, 30)).toBe(7);
		for (const name of ["big", "neg", "frac", "word", "missing"]) {
			expect(formInt(fields, name, 1, 30)).toBeUndefined();
		}
	});

	it("reads GiB sizes as bytes", () => {
		expect(formGiB(form({ size: "1.5" }), "size")).toBe(1.5 * 1024 ** 3);
		expect(formGiB(form({ size: "0" }), "size")).toBe(0);
		expect(formGiB(form({ size: "-1" }), "size")).toBeUndefined();
		expect(formGiB(form({ size: "lots" }), "size")).toBeUndefined();
		expect(toGiB(1.5 * 1024 ** 3)).toBe("1.5");
	});
});

describe("formatBytes", () => {
	it("picks a binary unit", () => {
		expect(formatBytes(7)).toBe("7 B");
		expect(formatBytes(12 * 1024)).toBe("12.0 KiB");
		expect(formatBytes(340 * 1024 ** 2)).toBe("340 MiB");
		expect(formatBytes(1.5 * 1024 ** 3)).toBe("1.5 GiB");
	});
});

describe("daysFromNow", () => {
	it("adds whole days", () => {
		const now = Date.UTC(2026, 10, 2, 12);
		expect(daysFromNow(3, now)).toBe("2026-11-05T12:00:00.000Z");
	});
});

describe("outcomeUrl", () => {
	it("keeps the page and query and replaces an earlier outcome", () => {
		const url = new URL("https://console.test/bans?username=bob&error=old");
		expect(outcomeUrl(url, "notice", "Ban revoked.")).toBe(
			"/bans?username=bob&notice=Ban+revoked.",
		);
	});
});
