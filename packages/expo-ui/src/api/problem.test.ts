import { describe, expect, it } from "vitest";

import { ApiError, apiErrorCode, ok } from "./problem";

describe("ApiError", () => {
	it("reads the API's problem body", () => {
		const error = new ApiError(
			400,
			"Bad Request",
			'{"status":400,"title":"Bad Request","code":"invalid_request","detail":"the reason is too long"}\n',
		);
		expect([error.status, error.code, error.message]).toEqual([
			400,
			"invalid_request",
			"the reason is too long",
		]);
	});

	it("keeps a validation problem's invalid fields apart from the message", () => {
		const error = new ApiError(
			422,
			"Unprocessable Entity",
			JSON.stringify({
				code: "validation_failed",
				detail: "validation failed",
				errors: [
					{ location: "body.title", message: "expected length <= 160" },
					{ message: "bad" },
				],
			}),
		);
		expect(error.message).toBe("validation failed");
		expect(error.invalid).toBe("body.title: expected length <= 160; bad");
	});

	it("keeps a problem's own string fields", () => {
		const error = new ApiError(
			409,
			"Conflict",
			'{"code":"preview_failed","detail":"x","failure_code":"entity_unknown","status":409}',
		);
		expect(error.fields).toEqual({ failure_code: "entity_unknown" });
	});

	it("shows the status text, not a proxy's own body", () => {
		const proxy = new ApiError(502, "Bad Gateway", " upstream down \n");
		expect([proxy.message, proxy.body]).toEqual(["Bad Gateway", " upstream down \n"]);
		expect(new ApiError(502, "Bad Gateway", "").message).toBe("Bad Gateway");
		const coded = new ApiError(500, "Internal", '{"code":1}');
		expect([coded.code, coded.message]).toEqual([undefined, "Internal"]);
	});
});

describe("ok", () => {
	it("unwraps a 2xx answer", async () => {
		const data = await ok(Promise.resolve({ data: { a: 1 }, response: new Response(null) }));
		expect(data).toEqual({ a: 1 });
	});

	it("throws the problem of any other answer", async () => {
		const call = Promise.resolve({
			error: { code: "not_found", detail: "no such entry" },
			response: new Response(null, { status: 404, statusText: "Not Found" }),
		});
		const error: unknown = await ok(call).catch((err: unknown) => err);
		expect(apiErrorCode(error)).toBe("not_found");
		expect(error instanceof ApiError && error.message).toBe("no such entry");
	});

	it("throws on an error status without a body", async () => {
		const call = Promise.resolve({
			response: new Response(null, { status: 503, statusText: "Service Unavailable" }),
		});
		const error: unknown = await ok(call).catch((err: unknown) => err);
		expect(error instanceof ApiError && [error.status, error.message]).toEqual([
			503,
			"Service Unavailable",
		]);
	});
});
