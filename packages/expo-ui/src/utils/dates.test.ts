import { describe, expect, it } from "vitest";
import {
	isIsoDate,
	isoDate,
	isoDateTime,
	localIsoDate,
	localIsoDateTime,
	remainingText,
	Temporal,
	upgradeTimes,
} from "./dates";

const value = "2026-10-20T23:30:00Z";

describe("dates", () => {
	it("has Temporal (native or the polyfill)", () => {
		expect(Temporal.Instant.from(value).epochMilliseconds).toBe(Date.UTC(2026, 9, 20, 23, 30));
	});

	it("formats UTC ISO dates", () => {
		expect(isoDate(value)).toBe("2026-10-20");
		expect(isoDateTime(value)).toBe("2026-10-20 23:30 UTC");
		expect(isoDateTime("2026-10-20T23:30:59.999+02:00")).toBe("2026-10-20 21:30 UTC");
	});

	it("formats in a time zone", () => {
		expect(localIsoDate(value, "Europe/Budapest")).toBe("2026-10-21");
		expect(localIsoDateTime(value, "Europe/Budapest")).toBe("2026-10-21 01:30");
		expect(localIsoDateTime(value, "UTC")).toBe("2026-10-20 23:30");
	});
});

describe("isIsoDate", () => {
	it("accepts real calendar dates only", () => {
		expect(isIsoDate("2026-10-20")).toBe(true);
		expect(isIsoDate("2028-02-29")).toBe(true);
		expect(isIsoDate("2026-02-29")).toBe(false);
		expect(isIsoDate("2026-13-01")).toBe(false);
		expect(isIsoDate("2026-1-1")).toBe(false);
		expect(isIsoDate("2026-10-20T10:00:00Z")).toBe(false);
		expect(isIsoDate("")).toBe(false);
	});
});

describe("remainingText", () => {
	const now = Temporal.Instant.from("2026-10-21T12:00:00Z");
	const after = (ms: number) => now.add({ milliseconds: ms });

	it("is undefined once past", () => {
		expect(remainingText(now, now)).toBeUndefined();
		expect(remainingText(after(-1), now)).toBeUndefined();
	});
	it("counts days, hours and minutes", () => {
		expect(remainingText(after(30_000), now)).toBe("less than a minute");
		expect(remainingText(after(12 * 60_000 + 59_000), now)).toBe("12 min");
		expect(remainingText(after(2 * 3_600_000), now)).toBe("2 hr");
		expect(remainingText(after(23 * 3_600_000 + 59 * 60_000), now)).toBe("23 hr, 59 min");
		expect(remainingText(after(7 * 86_400_000), now)).toBe("7 days");
		expect(remainingText(after(29 * 86_400_000 + 5 * 3_600_000 + 60_000), now)).toBe(
			"29 days, 5 hr",
		);
		expect(remainingText("2026-10-22T12:00:00Z", now)).toBe("1 day");
	});
});

describe("upgradeTimes", () => {
	it("keeps the server text for a bad timestamp", () => {
		const time = { dateTime: "nope", dataset: { local: "datetime" }, textContent: "kept" };
		upgradeTimes({ querySelectorAll: () => [time] } as unknown as ParentNode);
		expect(time.textContent).toBe("kept");
	});
});
