import { describe, expect, it } from "vitest";

import type { Entry } from "@lib/api";
import { entryView } from "./entry-view";

const entry = (over: Partial<Entry> = {}) =>
	({ visibility: "public", owner_user_id: "u1", spoiler: false, ...over }) as unknown as Entry;

describe("entryView", () => {
	it("public entry seen by a visitor", () => {
		expect(entryView(entry(), { canEdit: false })).toEqual({
			indexable: true,
			shareable: true,
			countsViews: true,
			sendsViewBeacon: true,
			reportable: true,
			pinnable: false,
			visibilityNote: undefined,
			showShareImage: true,
		});
	});

	it("owner of a public entry may pin, is not counted or reporting", () => {
		const view = entryView(entry(), { canEdit: true });
		expect(view).toMatchObject({ pinnable: true, sendsViewBeacon: false, reportable: false });
	});

	it("anonymous entries cannot be pinned", () => {
		expect(entryView(entry({ owner_user_id: undefined }), { canEdit: true }).pinnable).toBe(
			false,
		);
	});

	it("unlisted is shareable but not indexable", () => {
		const view = entryView(entry({ visibility: "unlisted" }), { canEdit: false });
		expect(view).toMatchObject({ indexable: false, shareable: true, countsViews: true });
		expect(view.visibilityNote).toMatch(/Unlisted/);
	});

	it("private is neither shareable nor counted", () => {
		const view = entryView(entry({ visibility: "private" }), { canEdit: true });
		expect(view).toMatchObject({
			indexable: false,
			shareable: false,
			countsViews: false,
			pinnable: false,
		});
		expect(view.visibilityNote).toMatch(/Private/);
	});

	it("quick previews are temporary: not indexed, counted, reported or pinned", () => {
		const view = entryView(entry({ quick_preview: true }), { canEdit: false });
		expect(view).toMatchObject({
			indexable: false,
			countsViews: false,
			reportable: false,
			pinnable: false,
		});
	});

	it("spoilers get no share image", () => {
		expect(entryView(entry({ spoiler: true }), { canEdit: false }).showShareImage).toBe(false);
	});
});
