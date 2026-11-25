import { describe, expect, it } from "vitest";
import { shownRevisionRawPath, toPublicAssetUrl } from "./asset-url";

describe("toPublicAssetUrl", () => {
	it("resolves API paths against an absolute base", () => {
		expect(toPublicAssetUrl("http://localhost:8080", "/api/v1/x.webp")).toBe(
			"http://localhost:8080/api/v1/x.webp",
		);
		expect(toPublicAssetUrl("https://a.test/base/", "/api/v1/x.webp")).toBe(
			"https://a.test/base/api/v1/x.webp",
		);
	});

	it("keeps API paths root-relative for a same-origin base", () => {
		expect(toPublicAssetUrl("/", "/api/v1/x.webp")).toBe("/api/v1/x.webp");
	});

	it("leaves absolute URLs alone", () => {
		expect(toPublicAssetUrl("/", "https://cdn.test/x.webp")).toBe("https://cdn.test/x.webp");
	});
});

describe("shownRevisionRawPath", () => {
	it("uses the entry's own route for the live revision", () => {
		expect(shownRevisionRawPath("abc", "r2", "r2")).toBe("/api/v1/entries/abc/raw");
	});

	it("names an older revision", () => {
		expect(shownRevisionRawPath("abc", "r1", "r2")).toBe(
			"/api/v1/entries/abc/revisions/r1/raw",
		);
	});
});
