import { describe, expect, it } from "vitest";

import { failureReason } from "./failure-reason";

describe("failureReason", () => {
	it("explains known codes", () => {
		expect(failureReason("request_too_large")).toBe("The blueprint is too large to render.");
		expect(failureReason("factorio_restarted")).toBe(
			"The renderer stopped while processing this blueprint.",
		);
	});
	it("has nothing for unknown or missing codes", () => {
		expect(failureReason("something_new")).toBeUndefined();
		expect(failureReason(undefined)).toBeUndefined();
		expect(failureReason("toString")).toBeUndefined();
	});
});
