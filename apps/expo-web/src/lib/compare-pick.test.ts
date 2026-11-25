import { describe, expect, it } from "vitest";
import { compareAction, pickLeaf, pickedLeaf } from "./compare-pick";

function memoryStorage() {
	const items = new Map<string, string>();
	return {
		getItem: (key: string) => items.get(key) ?? null,
		setItem: (key: string, value: string) => void items.set(key, value),
		removeItem: (key: string) => void items.delete(key),
	};
}

describe("picked blueprint", () => {
	it("is remembered and forgotten", () => {
		const storage = memoryStorage();
		expect(pickedLeaf(storage)).toBeUndefined();
		pickLeaf(storage, "abcdefghijkl");
		expect(pickedLeaf(storage)).toBe("abcdefghijkl");
		pickLeaf(storage);
		expect(pickedLeaf(storage)).toBeUndefined();
	});

	it("ignores anything that is not a blueprint id", () => {
		const storage = memoryStorage();
		storage.setItem("expo:compare-leaf", "../../admin");
		expect(pickedLeaf(storage)).toBeUndefined();
	});

	it("survives storage that throws", () => {
		const broken = {
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
		expect(pickedLeaf(broken)).toBeUndefined();
		expect(() => pickLeaf(broken, "abcdefghijkl")).not.toThrow();
	});
});

describe("compareAction", () => {
	it("picks first, opens the comparison on a second blueprint, and unpicks on the same one", () => {
		expect(compareAction(undefined, "bbbbbbbbbbbb")).toEqual({ kind: "pick" });
		expect(compareAction("aaaaaaaaaaaa", "aaaaaaaaaaaa")).toEqual({ kind: "clear" });
		expect(compareAction("aaaaaaaaaaaa", "bbbbbbbbbbbb")).toEqual({
			kind: "open",
			href: "/compare?a=aaaaaaaaaaaa&b=bbbbbbbbbbbb",
		});
	});
});
