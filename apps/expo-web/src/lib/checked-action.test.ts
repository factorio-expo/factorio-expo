import { describe, expect, it, vi } from "vitest";

import { performCheckedAction, turnstileField } from "./checked-action";
import type { HumanCheck } from "./turnstile";

const session = { signedOut: vi.fn<() => void>(), goToLogin: vi.fn<() => void>() };
const options = () => ({
	status: {
		textContent: "",
		setAttribute: () => {},
		removeAttribute: () => {},
	},
	fallback: "failed",
	onUnauthorized: "ignore" as const,
});

function fakeCheck(token: string | undefined) {
	const reset = vi.fn<() => void>();
	const check: HumanCheck = { token: () => token, waiting: () => "wait", reset };
	return { ...check, reset };
}

describe("performCheckedAction", () => {
	it("shows the waiting text and does not call until the check passes", async () => {
		const check = fakeCheck(undefined);
		const call = vi.fn<(token: string) => Promise<string>>();
		const opts = options();
		expect(await performCheckedAction(check, opts, call, session)).toBeUndefined();
		expect(call).not.toHaveBeenCalled();
		expect(opts.status.textContent).toBe("wait");
		expect(check.reset).not.toHaveBeenCalled();
	});

	it("passes the token and keeps the check after success", async () => {
		const check = fakeCheck("tok");
		const call = vi.fn<(token: string) => Promise<string>>().mockResolvedValue("ok");
		expect(await performCheckedAction(check, options(), call, session)).toBe("ok");
		expect(call).toHaveBeenCalledWith("tok");
		expect(check.reset).not.toHaveBeenCalled();
	});

	it("resets the check after a failed attempt", async () => {
		const check = fakeCheck("tok");
		const call = vi
			.fn<(token: string) => Promise<string>>()
			.mockRejectedValue(new Error("boom"));
		expect(await performCheckedAction(check, options(), call, session)).toBeUndefined();
		expect(check.reset).toHaveBeenCalledOnce();
	});

	it("runs with an empty token while the check is off", async () => {
		const call = vi.fn<(token: string) => Promise<boolean>>().mockResolvedValue(true);
		await performCheckedAction(fakeCheck(""), options(), call, session);
		expect(call).toHaveBeenCalledWith("");
	});
});

describe("turnstileField", () => {
	it("is empty without a token", () => {
		expect(turnstileField("")).toEqual({});
		expect(turnstileField("t")).toEqual({ turnstile_token: "t" });
	});
});
