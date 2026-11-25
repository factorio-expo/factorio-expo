import { describe, expect, it } from "vitest";

import { ApiError } from "@lib/api/http-client";
import {
	PENDING_CHANGE_MS,
	isReauthRequired,
	rememberPendingChange,
	takePendingChange,
} from "./fresh-sign-in";

function memoryStorage() {
	const items = new Map<string, string>();
	return {
		getItem: (key: string) => items.get(key) ?? null,
		setItem: (key: string, value: string) => void items.set(key, value),
		removeItem: (key: string) => void items.delete(key),
		items,
	};
}

describe("isReauthRequired", () => {
	it("recognises the API's reauth_required refusal", () => {
		const body = JSON.stringify({ code: "reauth_required", error: "confirm it's you" });
		expect(isReauthRequired(new ApiError(403, "Forbidden", body))).toBe(true);
	});

	it("ignores every other failure", () => {
		expect(isReauthRequired(new ApiError(403, "Forbidden", "you are banned"))).toBe(false);
		expect(isReauthRequired(new ApiError(403, "Forbidden", '{"code":"other"}'))).toBe(false);
		expect(
			isReauthRequired(new ApiError(401, "Unauthorized", '{"code":"reauth_required"}')),
		).toBe(false);
		expect(isReauthRequired(new Error("network"))).toBe(false);
	});
});

describe("pending change", () => {
	it("is taken once", () => {
		const storage = memoryStorage();
		rememberPendingChange({ kind: "remove-passkey", id: "abc" }, 1000, storage);
		expect(takePendingChange(2000, storage)).toEqual({ kind: "remove-passkey", id: "abc" });
		expect(takePendingChange(2000, storage)).toBeUndefined();
	});

	it("keeps a data download", () => {
		const storage = memoryStorage();
		rememberPendingChange({ kind: "export" }, 1000, storage);
		expect(takePendingChange(2000, storage)).toEqual({ kind: "export" });
	});

	it("keeps a new recovery code", () => {
		const storage = memoryStorage();
		rememberPendingChange({ kind: "recovery-code" }, 1000, storage);
		expect(takePendingChange(2000, storage)).toEqual({ kind: "recovery-code" });
	});

	it("keeps an account deletion", () => {
		const storage = memoryStorage();
		rememberPendingChange({ kind: "delete-account" }, 1000, storage);
		expect(takePendingChange(2000, storage)).toEqual({ kind: "delete-account" });
	});

	it("expires with the fresh sign-in window", () => {
		const storage = memoryStorage();
		rememberPendingChange({ kind: "link", provider: "github" }, 0, storage);
		expect(takePendingChange(PENDING_CHANGE_MS + 1, storage)).toBeUndefined();
		expect(storage.items.size).toBe(0);
	});

	it("drops anything that is not a change", () => {
		const storage = memoryStorage();
		for (const raw of ["not json", '{"at":0}', '{"change":{"kind":"delete-all"},"at":0}']) {
			storage.setItem("expo:pending-account-change", raw);
			expect(takePendingChange(1, storage)).toBeUndefined();
		}
	});

	it("survives missing storage", () => {
		expect(() => rememberPendingChange({ kind: "add-passkey" }, 0, null)).not.toThrow();
		expect(takePendingChange(0, null)).toBeUndefined();
	});
});
