import { describe, expect, it } from "vitest";

import {
	RECOVERY_CODE_PATH,
	destinationAfterSignIn,
	forgetRecoveryCode,
	readRecoveryCode,
	recoveryCodeFile,
	recoveryCodeFileName,
	rememberRecoveryCode,
} from "./recovery-code";

function memoryStorage() {
	const items = new Map<string, string>();
	return {
		getItem: (key: string) => items.get(key) ?? null,
		setItem: (key: string, value: string) => void items.set(key, value),
		removeItem: (key: string) => void items.delete(key),
		items,
	};
}

const pending = {
	code: "ABCDE-FGHJK-LMNPQ-RSTUV-WXYZ2-3",
	username: "alice",
	reason: "signup" as const,
	next: "/account/favorites",
};

describe("recovery code hand-off", () => {
	it("stays readable until it is forgotten", () => {
		const storage = memoryStorage();
		expect(rememberRecoveryCode(pending, storage)).toBe(true);
		expect(readRecoveryCode(storage)).toEqual(pending);
		expect(readRecoveryCode(storage)).toEqual(pending);
		forgetRecoveryCode(storage);
		expect(readRecoveryCode(storage)).toBeUndefined();
	});

	it("keeps a code asked for on the account page", () => {
		const storage = memoryStorage();
		const asked = { ...pending, reason: "regenerate" as const, next: "/account" };
		rememberRecoveryCode(asked, storage);
		expect(readRecoveryCode(storage)).toEqual(asked);
	});

	it("ignores a stored value that is not a code of this shape", () => {
		for (const raw of [
			"not json",
			"null",
			"[]",
			JSON.stringify({ ...pending, reason: "other" }),
			JSON.stringify({ ...pending, code: 12 }),
			JSON.stringify({ username: "alice" }),
		]) {
			const storage = memoryStorage();
			storage.setItem("expo:recovery-code", raw);
			expect(readRecoveryCode(storage)).toBeUndefined();
		}
	});

	it("works without storage", () => {
		const blocked = {
			getItem: () => {
				throw new Error("blocked");
			},
			setItem: () => {
				throw new Error("blocked");
			},
			removeItem: () => {
				throw new Error("blocked");
			},
		};
		expect(rememberRecoveryCode(pending, blocked)).toBe(false);
		expect(rememberRecoveryCode(pending, null)).toBe(false);
		expect(readRecoveryCode(blocked)).toBeUndefined();
		expect(() => forgetRecoveryCode(blocked)).not.toThrow();
	});
});

describe("destinationAfterSignIn", () => {
	it("shows the code first and keeps where the person was going", () => {
		const storage = memoryStorage();
		const where = destinationAfterSignIn(
			{ username: "alice", recovery_code: pending.code },
			"signup",
			"/account/favorites",
			storage,
		);
		expect(where).toBe(RECOVERY_CODE_PATH);
		expect(readRecoveryCode(storage)).toEqual(pending);
	});

	it("goes straight on when the response has no code", () => {
		const storage = memoryStorage();
		expect(destinationAfterSignIn({ username: "alice" }, "signup", "/", storage)).toBe("/");
		expect(readRecoveryCode(storage)).toBeUndefined();
	});

	it("goes straight on when the code cannot be kept for the page", () => {
		expect(
			destinationAfterSignIn(
				{ username: "alice", recovery_code: pending.code },
				"recovery",
				"/account",
				null,
			),
		).toBe("/account");
	});
});

describe("recovery code file", () => {
	it("holds the username, the code and where to use it", () => {
		const text = recoveryCodeFile(pending, "https://expo.example");
		expect(text).toContain("Username: alice");
		expect(text).toContain(`Recovery code: ${pending.code}`);
		expect(text).toContain("https://expo.example/recover");
	});

	it("is named after the account", () => {
		expect(recoveryCodeFileName("alice")).toBe("factorio-expo-recovery-alice.txt");
	});
});
