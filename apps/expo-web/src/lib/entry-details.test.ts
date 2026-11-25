import { describe, expect, it } from "vitest";
import {
	commentPurgeWarning,
	readEntryDetails,
	SPOILER_FIELD,
	TAG_PICKER_FIELD,
} from "./entry-details";

const form = (fields: [string, string][]) => {
	const data = new FormData();
	for (const [name, value] of fields) data.append(name, value);
	return data;
};

describe("readEntryDetails", () => {
	it("trims the title and description", () => {
		expect(
			readEntryDetails(
				form([
					["title", "  Smelter "],
					["description", "\nfast\n"],
				]),
			),
		).toEqual({ title: "Smelter", description: "fast" });
	});

	it("sends no tags key when the tag picker was not rendered", () => {
		const details = readEntryDetails(form([["title", "t"]]));
		expect(details).not.toHaveProperty("tags");
	});

	it("sends an empty tag list when the picker was rendered with nothing checked", () => {
		expect(readEntryDetails(form([[TAG_PICKER_FIELD, ""]])).tags).toEqual([]);
	});

	it("sends every checked tag in form order", () => {
		const details = readEntryDetails(
			form([
				[TAG_PICKER_FIELD, ""],
				["tags", "smelting"],
				["tags", "early-game"],
			]),
		);
		expect(details.tags).toEqual(["smelting", "early-game"]);
	});

	it("sends the spoiler flag only when the checkbox was rendered", () => {
		expect(readEntryDetails(form([[SPOILER_FIELD, ""]])).spoiler).toBe(false);
		expect(
			readEntryDetails(
				form([
					[SPOILER_FIELD, ""],
					["spoiler", "on"],
				]),
			).spoiler,
		).toBe(true);
		expect(readEntryDetails(form([]))).not.toHaveProperty("spoiler");
	});

	it("keeps only known visibilities", () => {
		expect(readEntryDetails(form([["visibility", "unlisted"]])).visibility).toBe("unlisted");
		expect(readEntryDetails(form([["visibility", "secret"]]))).not.toHaveProperty("visibility");
		expect(readEntryDetails(form([]))).not.toHaveProperty("visibility");
	});

	it("reads the lifetime, with keep and a missing select sending nothing", () => {
		expect(readEntryDetails(form([["expires_in_days", "0"]])).expires_in_days).toBe(0);
		expect(readEntryDetails(form([["expires_in_days", "7"]])).expires_in_days).toBe(7);
		expect(readEntryDetails(form([["expires_in_days", ""]]))).not.toHaveProperty(
			"expires_in_days",
		);
		expect(readEntryDetails(form([]))).not.toHaveProperty("expires_in_days");
	});
});

describe("commentPurgeWarning", () => {
	it("says nothing when the entry stays public or has no comments", () => {
		expect(commentPurgeWarning(undefined, 3)).toBeUndefined();
		expect(commentPurgeWarning("public", 3)).toBeUndefined();
		expect(commentPurgeWarning("private", 0)).toBeUndefined();
	});

	it("counts the comments a non-public visibility deletes", () => {
		expect(commentPurgeWarning("private", 1)).toContain("deletes its 1 comment for good");
		expect(commentPurgeWarning("unlisted", 2)).toContain(
			"making it unlisted deletes its 2 comments",
		);
	});
});

describe("readEntryDetails alias", () => {
	it("sends the alias only when the field was rendered, trimmed and lower-cased", () => {
		expect(readEntryDetails(form([["title", "t"]]))).not.toHaveProperty("alias");
		expect(readEntryDetails(form([["alias", "  Iron-Smelter "]])).alias).toBe("iron-smelter");
	});

	it("sends an empty alias when the field was cleared, so the alias is removed", () => {
		expect(readEntryDetails(form([["alias", ""]])).alias).toBe("");
	});
});
