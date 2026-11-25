import { describe, expect, it } from "vitest";

import { ApiError } from "@lib/api/http-client";
import { failureText, performAction, type ActionOptions, type UnauthorizedPolicy } from "./action";

const apiError = (status: number, error?: string, code = "x") =>
	new ApiError(
		status,
		"Status",
		error === undefined ? "" : JSON.stringify({ code, detail: error }),
	);

/** A status line and a session that record what the runner did to them. */
function harness() {
	const attributes = new Map<string, string>();
	const status = {
		textContent: "",
		setAttribute: (name: string, value: string) => void attributes.set(name, value),
		removeAttribute: (name: string) => void attributes.delete(name),
	};
	const session = { signedOut: 0, login: 0 };
	const effects = {
		signedOut: () => void session.signedOut++,
		goToLogin: () => void session.login++,
	};
	const control = { disabled: false };
	const run = <T>(
		options: Omit<ActionOptions, "fallback" | "onUnauthorized"> & {
			onUnauthorized?: UnauthorizedPolicy;
		},
		call: () => Promise<T>,
	) =>
		performAction(
			{ fallback: "It failed.", onUnauthorized: "clear", controls: [control], ...options },
			call,
			effects,
		);
	return { status, attributes, session, control, run };
}

describe("failureText", () => {
	it("prefers a fixed text for the status, then the API's text, then the fallback", () => {
		expect(failureText(apiError(404, "nope"), "fb", { 404: "Not found." })).toBe("Not found.");
		expect(failureText(apiError(400, "title is too long\n"), "fb")).toBe("title is too long");
		expect(failureText(apiError(500), "fb")).toBe("fb");
		expect(failureText(new TypeError("Failed to fetch"), "fb")).toBe("fb");
	});
});

describe("performAction", () => {
	it("disables controls and shows the busy text while the call runs", async () => {
		const h = harness();
		let during = { disabled: false, text: "", busy: "" };
		const value = await h.run({ status: h.status, busy: "Saving..." }, async () => {
			during = {
				disabled: h.control.disabled,
				text: h.status.textContent,
				busy: h.attributes.get("aria-busy") ?? "",
			};
			return 7;
		});
		expect(value).toBe(7);
		expect(during).toEqual({ disabled: true, text: "Saving...", busy: "true" });
		// A page that navigates away keeps its controls disabled and the busy text.
		expect(h.control.disabled).toBe(true);
		expect(h.status.textContent).toBe("Saving...");
	});

	it("re-enables controls and clears the busy text on success when the page stays", async () => {
		const h = harness();
		await h.run({ status: h.status, busy: "Saving...", restore: true }, async () => true);
		expect(h.control.disabled).toBe(false);
		expect(h.status.textContent).toBe("");
		expect(h.attributes.has("aria-busy")).toBe(false);
	});

	it("always surfaces a failure, re-enables controls and undoes the change", async () => {
		const h = harness();
		let undone = false;
		const value = await h.run({ status: h.status, onFailure: () => (undone = true) }, () =>
			Promise.reject(apiError(409, "already there")),
		);
		expect(value).toBeUndefined();
		expect(undone).toBe(true);
		expect(h.control.disabled).toBe(false);
		expect(h.status.textContent).toBe("already there");
	});

	it("reports through report() when there is no status line", async () => {
		const h = harness();
		const reported: string[] = [];
		await h.run({ report: (message) => reported.push(message) }, () =>
			Promise.reject(new TypeError("offline")),
		);
		expect(reported).toEqual(["It failed."]);
	});

	it.each([
		["login", { signedOut: 1, login: 1, text: "" }],
		["clear", { signedOut: 1, login: 0, text: "session expired" }],
		["ignore", { signedOut: 0, login: 0, text: "session expired" }],
	] as const)("applies the %s policy to a 401", async (policy, want) => {
		const h = harness();
		await h.run({ status: h.status, onUnauthorized: policy }, () =>
			Promise.reject(apiError(401, "session expired")),
		);
		expect({ ...h.session, text: h.status.textContent }).toEqual(want);
	});

	it("leaves the session alone for other failures", async () => {
		const h = harness();
		await h.run({ status: h.status, onUnauthorized: "login" }, () =>
			Promise.reject(apiError(403)),
		);
		expect(h.session).toEqual({ signedOut: 0, login: 0 });
		expect(h.status.textContent).toBe("It failed.");
	});
});
