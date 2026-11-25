import { describe, expect, it } from "vitest";

import { ApiError, apiErrorCode } from "@lib/api/http-client";
import { RENDER_QUEUE_FULL, explainApiError } from "./api-errors";
import {
	ANONYMOUS_UPLOADS_PAUSED,
	COMMENTS_PAUSED,
	SIGNUPS_PAUSED,
	UPLOADS_PAUSED,
} from "./site-pause";

const refusal = (status: number, body: object) => new ApiError(status, "", JSON.stringify(body));
const coded = (status: number, code: string) => refusal(status, { code, detail: "x" });

describe("ApiError", () => {
	it("reads the API's error body", () => {
		const err = refusal(422, {
			status: 422,
			title: "Unprocessable Entity",
			code: "preview_failed",
			detail: " failed \n",
			failure_code: "new",
		});
		expect([err.code, err.detail, err.fields]).toEqual([
			"preview_failed",
			"failed",
			{ failure_code: "new" },
		]);
		expect(apiErrorCode(err)).toBe("preview_failed");
	});

	it("keeps a validation problem's invalid fields out of the detail", () => {
		const err = refusal(422, {
			status: 422,
			code: "validation_failed",
			detail: "validation failed",
			errors: [
				{ message: "expected length <= 160", location: "body.title" },
				{ message: "unexpected property" },
			],
		});
		expect([err.code, err.detail]).toEqual(["validation_failed", "validation failed"]);
		expect(err.invalid).toBe("body.title: expected length <= 160; unexpected property");
	});

	it("leaves a body that is not the API's alone", () => {
		const err = new ApiError(502, "Bad Gateway", "<html>proxy</html>");
		expect([err.code, err.detail]).toEqual([undefined, undefined]);
		expect(apiErrorCode(new Error("x"))).toBeUndefined();
	});
});

describe("explainApiError", () => {
	it("words each paused action", () => {
		expect(explainApiError(coded(503, "uploads_paused"))).toBe(UPLOADS_PAUSED);
		expect(explainApiError(coded(503, "anonymous_uploads_paused"))).toBe(
			ANONYMOUS_UPLOADS_PAUSED,
		);
		expect(explainApiError(coded(503, "signups_paused"))).toBe(SIGNUPS_PAUSED);
		expect(explainApiError(coded(503, "comments_paused"))).toBe(COMMENTS_PAUSED);
	});

	it("words the human check codes", () => {
		expect(explainApiError(coded(403, "turnstile_failed"))).toMatch(/did not pass/);
		expect(explainApiError(coded(503, "turnstile_unavailable"))).toMatch(/not working/);
	});

	it("explains a full render queue", () => {
		expect(explainApiError(coded(429, "render_queue_full"))).toBe(RENDER_QUEUE_FULL);
	});

	it("names the failure a refused quick preview already had", () => {
		expect(
			explainApiError(
				refusal(422, {
					code: "preview_failed",
					detail: "x",
					failure_code: "factorio_restarted",
				}),
			),
		).toBe(
			"This blueprint already failed to render, so it was not queued again: The renderer stopped while processing this blueprint.",
		);
		expect(
			explainApiError(
				refusal(422, { code: "preview_failed", detail: "x", failure_code: "new" }),
			),
		).toBe("This blueprint already failed to render, so it was not queued again.");
	});

	it("leaves every other error to the API's own text", () => {
		expect(explainApiError(coded(409, "username_taken"))).toBeUndefined();
		expect(explainApiError(coded(403, "reauth_required"))).toBeUndefined();
		expect(explainApiError(coded(400, "toString"))).toBeUndefined();
		expect(explainApiError(new TypeError("offline"))).toBeUndefined();
	});
});
