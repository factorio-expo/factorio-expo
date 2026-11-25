import { describe, expect, it } from "vitest";

import { anonymousCacheControl } from "./page-cache";

describe("anonymousCacheControl", () => {
	it("caches the home and static pages at the edge only", () => {
		expect(anonymousCacheControl("/")).toContain("s-maxage=600");
		expect(anonymousCacheControl("/about")).toContain("s-maxage=600");
		expect(anonymousCacheControl("/privacy-policy")).toContain("max-age=0");
		expect(anonymousCacheControl("/terms-of-use")).toBeDefined();
	});

	it("leaves every other page uncached", () => {
		for (const path of [
			"/browse",
			"/login",
			"/upload",
			"/e/abc123def456",
			"/about/x",
			"/account",
		]) {
			expect(anonymousCacheControl(path)).toBeUndefined();
		}
	});
});
