import { describe, expect, it } from "vitest";

import { problemContextOf, problemContextRows } from "./problem-report";

describe("problemContextOf", () => {
	it("merges the entry page's marker with the window's facts", () => {
		const marker = JSON.stringify({
			entry_id: "abcdefghijkl",
			blueprint_id: "mnopqrstuvwx",
			context: { game_version: "2.0.72", preview_failure: "", odd: 3 },
		});
		expect(
			problemContextOf("/e/abcdefghijkl/b/mnopqrstuvwx", marker, {
				viewport: "800x600",
				theme: "",
			}),
		).toEqual({
			page: "/e/abcdefghijkl/b/mnopqrstuvwx",
			entry_id: "abcdefghijkl",
			blueprint_id: "mnopqrstuvwx",
			context: { game_version: "2.0.72", viewport: "800x600" },
		});
	});

	it("keeps the path alone without a marker or with a broken one", () => {
		expect(problemContextOf("/about", undefined, {})).toEqual({ page: "/about" });
		expect(problemContextOf("/about", "{not json", { viewport: "1x1" })).toEqual({
			page: "/about",
			context: { viewport: "1x1" },
		});
	});
});

describe("problemContextRows", () => {
	it("labels known keys and keeps unknown ones as they are", () => {
		expect(
			problemContextRows({
				page: "/e/x",
				entry_id: "x",
				context: { game_version: "2.0", custom: "y" },
			}),
		).toEqual([
			["Page", "/e/x"],
			["Entry", "x"],
			["Game version", "2.0"],
			["custom", "y"],
		]);
	});
});
