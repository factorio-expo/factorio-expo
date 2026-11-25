import { describe, expect, it } from "vitest";
import { ApiError, type Entry } from "@lib/api";
import { loadEntryAccess, unlessMissing } from "./entry-access";

const entry = (fields: Partial<Entry> = {}) =>
	({ id: "abc", owner_user_id: "owner", quick_preview: false, ...fields }) as Entry;

const api = (got: Promise<Entry>, canEdit: Promise<boolean>) => ({
	entries: {
		get: () => got,
		permissions: () => canEdit.then((can_edit) => ({ can_edit })),
	},
});

const missing = () => Promise.reject(new ApiError(404, "Not Found", ""));

describe("loadEntryAccess", () => {
	it("lets the owner edit an owned entry", async () => {
		const access = await loadEntryAccess(
			api(Promise.resolve(entry()), Promise.resolve(true)),
			"abc",
		);
		expect(access).toMatchObject({ creator: true, canEdit: true, claimable: false });
	});

	it("never lets anyone edit a quick preview; its creator may publish it", async () => {
		const access = await loadEntryAccess(
			api(Promise.resolve(entry({ quick_preview: true })), Promise.resolve(true)),
			"abc",
		);
		expect(access).toMatchObject({ creator: true, canEdit: false, claimable: false });
	});

	it("lets an edit-link holder edit and claim an anonymous entry", async () => {
		const access = await loadEntryAccess(
			api(Promise.resolve(entry({ owner_user_id: undefined })), Promise.resolve(true)),
			"abc",
		);
		expect(access).toMatchObject({ creator: true, canEdit: true, claimable: true });
	});

	it("treats a failed permissions check as no rights", async () => {
		const access = await loadEntryAccess(
			api(Promise.resolve(entry()), Promise.reject(new Error("down"))),
			"abc",
		);
		expect(access).toMatchObject({ creator: false, canEdit: false, claimable: false });
	});

	it("is undefined for a missing entry and rethrows other errors", async () => {
		expect(
			await loadEntryAccess(api(missing(), Promise.resolve(false)), "abc"),
		).toBeUndefined();
		await expect(
			loadEntryAccess(
				api(Promise.reject(new ApiError(500, "Oops", "")), Promise.resolve(false)),
				"abc",
			),
		).rejects.toMatchObject({ status: 500 });
	});
});

describe("unlessMissing", () => {
	it("turns only a 404 into undefined", async () => {
		expect(await unlessMissing(Promise.resolve(1))).toBe(1);
		expect(await unlessMissing(missing())).toBeUndefined();
		await expect(unlessMissing(Promise.reject(new Error("x")))).rejects.toThrow("x");
	});
});
