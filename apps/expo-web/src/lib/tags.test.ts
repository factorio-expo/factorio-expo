import { describe, expect, it } from "vitest";
import type { TagGroup } from "./api";
import { tagHref, tagRefs } from "./tags";

const groups: TagGroup[] = [
	{
		id: "g1",
		slug: "logistics",
		display_name: "Logistics",
		tags: [
			{
				id: "t1",
				slug: "lane-balancer",
				display_name: "Lane balancer",
				icon: "[item=splitter]",
			},
		],
	},
];

describe("tagRefs", () => {
	it("maps slugs to names and keeps unknown slugs", () => {
		expect(tagRefs(groups, ["lane-balancer", "retired-tag"])).toEqual([
			{ slug: "lane-balancer", name: "Lane balancer", icon: "[item=splitter]" },
			{ slug: "retired-tag", name: "retired-tag", icon: "" },
		]);
	});
});

describe("tagHref", () => {
	it("links to the browse page filtered by the tag", () => {
		expect(tagHref("lane-balancer")).toBe("/browse?tags=lane-balancer");
	});
});
