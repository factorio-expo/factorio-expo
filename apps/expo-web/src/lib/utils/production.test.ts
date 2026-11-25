import { describe, expect, it } from "vitest";
import {
	formatAmount,
	formatPower,
	formatRate,
	formatSize,
	formatSnapToGrid,
	formatStringSize,
	productionTimescales,
	rateDivisors,
} from "./production";

describe("formatRate", () => {
	it("rounds rates to two decimals and ratios to three significant digits", () => {
		expect(formatRate(1.23456)).toBe("1.23/s");
		expect(formatRate(0.0012345, undefined, "item", 1, true)).toBe("0.00123/s");
		expect(formatRate(0.5, productionTimescales[1], "item", 2)).toBe("60/min");
	});
});

describe("rateDivisors", () => {
	it("uses the footprint area and the consumption in MW", () => {
		expect(
			rateDivisors({
				availability: "ready",
				stale: false,
				footprint: { width: 4, height: 5 },
				electricity: { production_watts: 0, consumption_watts: 500000, net_watts: -500000 },
			}),
		).toEqual({ tiles: 20, megawatts: 0.5 });
	});

	it("leaves out what is unknown or not positive", () => {
		expect(
			rateDivisors({
				availability: "ready",
				stale: false,
				electricity: { production_watts: 1e6, consumption_watts: 0, net_watts: 1e6 },
			}),
		).toEqual({ tiles: undefined, megawatts: undefined });
	});
});

describe("formatPower", () => {
	it("shows whole kilowatts", () => {
		expect(formatPower(1_800_000)).toBe("1,800 kW");
		expect(formatPower(-90_000)).toBe("-90 kW");
	});
});

describe("formatStringSize", () => {
	it("counts characters, which are bytes in an exchange string", () => {
		expect(formatStringSize("0eNq")).toBe("4 characters (4 B)");
		expect(formatStringSize("a".repeat(12345))).toBe("12,345 characters (12.3 kB)");
	});
});

describe("formatAmount", () => {
	it("keeps small amounts readable and shortens large ones", () => {
		expect(formatAmount(3)).toBe("3");
		expect(formatAmount(0.25)).toBe("0.3");
		expect(formatAmount(39.5)).toBe("40");
		expect(formatAmount(999)).toBe("999");
		expect(formatAmount(12345)).toBe("12.3K");
	});
});

describe("formatSize", () => {
	it("joins width and height", () => {
		expect(formatSize(12, 7)).toBe("12 × 7");
	});
});

describe("formatSnapToGrid", () => {
	it("names the grid size and position", () => {
		expect(formatSnapToGrid(undefined)).toBe("Off");
		expect(
			formatSnapToGrid({ width: 32, height: 16, absolute: true, offset_x: 3, offset_y: 7 }),
		).toBe("32 × 16 tiles, absolute (offset 3, 7)");
		expect(
			formatSnapToGrid({ width: 2, height: 2, absolute: false, offset_x: 0, offset_y: 0 }),
		).toBe("2 × 2 tiles, relative");
	});
});
