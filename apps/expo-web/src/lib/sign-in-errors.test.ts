import { describe, expect, it } from "vitest";
import { signInErrorText } from "./sign-in-errors";

describe("signInErrorText", () => {
	it("has no text without a code", () => {
		expect(signInErrorText(null)).toBeUndefined();
		expect(signInErrorText("")).toBeUndefined();
	});

	it("explains the API's codes", () => {
		expect(signInErrorText("identity-taken")).toMatch(/already linked to another/);
		expect(signInErrorText("cancelled")).toBe("Sign-in was cancelled.");
		expect(signInErrorText("identity-held")).toMatch(/once the ban ends/);
	});

	it("falls back to a generic failure for an unknown code", () => {
		expect(signInErrorText("<script>")).toBe(signInErrorText("failed"));
	});
});
