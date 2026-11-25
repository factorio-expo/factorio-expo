import { describe, expect, it } from "vitest";

import { versionBadge } from "./version-badge";

describe("versionBadge", () => {
	it("marks a 2.1 blueprint", () => {
		expect(versionBadge("2.1.24", "")).toEqual({
			text: "2.1",
			tone: "native",
			title: "Made in Factorio 2.1",
		});
	});
	it("shows the compatibility of an older one", () => {
		expect(versionBadge("2.0.15", "compatible")?.text).toBe("2.1 compatible");
		expect(versionBadge("2.0.15", "incompatible")?.text).toBe("2.1 incompatible");
		expect(versionBadge("2.0.15", "compatible")?.title).toBe(
			"Made before 2.1: compatible with Factorio 2.1",
		);
	});
	it("shows nothing without a version or a verdict", () => {
		expect(versionBadge("", "")).toBeUndefined();
		expect(versionBadge("2.0.15", "")).toBeUndefined();
	});
});
