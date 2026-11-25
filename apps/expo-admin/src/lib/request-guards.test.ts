import { describe, expect, it } from "vitest";

import { isAllowedOrigin } from "./request-guards";

describe("isAllowedOrigin", () => {
	const origin = "https://console.example.test";
	it("lets reads through", () => {
		expect(isAllowedOrigin("GET", new Headers(), origin)).toBe(true);
	});
	it("needs the console origin on writes", () => {
		expect(isAllowedOrigin("POST", new Headers({ Origin: origin }), origin)).toBe(true);
		expect(isAllowedOrigin("POST", new Headers(), origin)).toBe(false);
		expect(isAllowedOrigin("POST", new Headers({ Origin: "https://evil.test" }), origin)).toBe(
			false,
		);
		expect(
			isAllowedOrigin(
				"POST",
				new Headers({ Origin: origin, "Sec-Fetch-Site": "cross-site" }),
				origin,
			),
		).toBe(false);
	});
	it("trusts Sec-Fetch-Site over a null Origin", () => {
		const posted = (site: string) =>
			isAllowedOrigin(
				"POST",
				new Headers({ Origin: "null", "Sec-Fetch-Site": site }),
				origin,
			);
		expect(posted("same-origin")).toBe(true);
		expect(posted("same-site")).toBe(false);
		expect(posted("cross-site")).toBe(false);
		expect(posted("none")).toBe(false);
		expect(isAllowedOrigin("POST", new Headers({ Origin: "null" }), origin)).toBe(false);
	});
});
