import { describe, expect, it } from "vitest";
import { signInMethods } from "./format";

describe("signInMethods", () => {
	it("names providers and counts passkeys", () => {
		expect(signInMethods({ providers: ["github", "steam"], passkeys: 2 })).toBe(
			"GitHub, Steam, 2 passkeys",
		);
		expect(signInMethods({ providers: [], passkeys: 1 })).toBe("1 passkey");
		expect(signInMethods({ providers: ["newcomer"], passkeys: 0 })).toBe("newcomer");
		expect(signInMethods({ providers: [], passkeys: 0 })).toBe("none");
	});
});
