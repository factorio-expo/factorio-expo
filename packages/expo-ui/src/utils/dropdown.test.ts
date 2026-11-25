import { describe, expect, it } from "vitest";

import { dropdownsOutside, type DropdownHandle } from "./dropdown";

const fake = (inside: object): DropdownHandle => ({
	contains: (node) => node === inside,
	close: () => {},
});

describe("dropdownsOutside", () => {
	it("returns every dropdown the click is outside of", () => {
		const a = {};
		const b = {};
		const [first, second] = [fake(a), fake(b)];
		expect(dropdownsOutside([first, second], a as Node)).toEqual([second]);
		expect(dropdownsOutside([first, second], {} as Node)).toEqual([first, second]);
	});
});
