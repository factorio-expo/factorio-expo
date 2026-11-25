import { describe, expect, it } from "vitest";

import { setSecurityHeaders } from "./security-headers";

describe("setSecurityHeaders", () => {
	it("keeps Astro's CSP and adds the framing policy as a second one", () => {
		const headers = new Headers({ "Content-Security-Policy": "script-src 'self'" });
		setSecurityHeaders(headers);
		expect(headers.get("Content-Security-Policy")).toBe(
			"script-src 'self', frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'",
		);
		expect(headers.get("X-Frame-Options")).toBe("DENY");
		expect(headers.get("X-Content-Type-Options")).toBe("nosniff");
	});
});
