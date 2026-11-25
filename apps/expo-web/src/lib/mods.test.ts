import { describe, expect, it } from "vitest";

import { modBadgeRefs, modHref } from "./mods";

describe("modBadgeRefs", () => {
	it("titles the filter values and links each to its search", () => {
		const badges = modBadgeRefs(
			["space-age", "krastorio2", "mystery"],
			[{ name: "krastorio2", title: "Krastorio 2" }],
		);
		expect(badges).toEqual([
			{ value: "space-age", label: "Space Age", href: "/browse?mods=space-age" },
			{ value: "krastorio2", label: "Krastorio 2", href: "/browse?mods=krastorio2" },
			{ value: "mystery", label: "mystery", href: "/browse?mods=mystery" },
		]);
	});

	it("encodes the value in the link", () => {
		expect(modHref("a b")).toBe("/browse?mods=a%20b");
	});
});
