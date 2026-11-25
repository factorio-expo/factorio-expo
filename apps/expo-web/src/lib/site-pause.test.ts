import { describe, expect, it } from "vitest";

import { ANONYMOUS_UPLOADS_PAUSED, OPEN_SITE, UPLOADS_PAUSED, uploadPause } from "./site-pause";

describe("uploadPause", () => {
	it("is undefined while everything is open", () => {
		expect(uploadPause(OPEN_SITE, false)).toBeUndefined();
	});
	it("pauses everyone with the all-uploads switch", () => {
		expect(uploadPause({ ...OPEN_SITE, uploads: false }, true)).toBe(UPLOADS_PAUSED);
	});
	it("pauses only signed-out visitors with the anonymous switch", () => {
		const site = { ...OPEN_SITE, anonymous_uploads: false };
		expect(uploadPause(site, false)).toBe(ANONYMOUS_UPLOADS_PAUSED);
		expect(uploadPause(site, true)).toBeUndefined();
	});
});
