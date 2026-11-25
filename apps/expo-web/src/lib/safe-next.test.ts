import { describe, expect, it } from "vitest";

import { loginPath, safeNext } from "./safe-next";

const origin = "https://example.test";

describe("safeNext", () => {
	it("keeps same-origin paths with query and hash", () => {
		expect(safeNext("/account/favorites?x=1#top", origin)).toBe("/account/favorites?x=1#top");
	});
	it("rejects other origins and non-paths", () => {
		expect(safeNext("//evil.test/x", origin)).toBe("/");
		expect(safeNext("/\t/evil.test", origin)).toBe("/");
		expect(safeNext("https://evil.test", origin)).toBe("/");
		expect(safeNext("javascript:alert(1)", origin)).toBe("/");
		expect(safeNext(null, origin)).toBe("/");
		expect(safeNext("", origin)).toBe("/");
	});
});

describe("loginPath", () => {
	it("encodes the destination", () => {
		expect(loginPath("/a?b=c")).toBe("/login?next=%2Fa%3Fb%3Dc");
	});
});
