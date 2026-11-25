import { describe, expect, it } from "vitest";
import { editLinkUrl, editTokenOf, editorLinks, entryPath, leafPath } from "./entry-links";

describe("editTokenOf", () => {
	it("reads the token from a page URL", () => {
		expect(editTokenOf("https://expo.test/e/abc?token=t%2Fk")).toBe("t/k");
		expect(editTokenOf(new URL("https://expo.test/e/abc"))).toBeUndefined();
		expect(editTokenOf("https://expo.test/e/abc?token=")).toBeUndefined();
	});
});

describe("entry links", () => {
	it("carry the revision only when given", () => {
		expect(entryPath("abc")).toBe("/e/abc");
		expect(entryPath("abc", "r1")).toBe("/e/abc?revision=r1");
		expect(leafPath("abc", "leaf")).toBe("/e/abc/b/leaf");
	});

	it("carry the page's edit token as the editor follows them", () => {
		const editor = editorLinks("https://expo.test/e/abc?token=t%2Fk%2Bn");
		expect(editor.token).toBe("t/k+n");
		expect(editor.entry("abc", "r1")).toBe("/e/abc?revision=r1&token=t%2Fk%2Bn");
		expect(editor.leaf("abc", "leaf")).toBe("/e/abc/b/leaf?token=t%2Fk%2Bn");
		expect(editor.newRevision("abc")).toBe("/e/abc/revisions/new?token=t%2Fk%2Bn");
	});

	it("carry nothing from a page without a token", () => {
		const viewer = editorLinks(new URL("https://expo.test/e/abc?revision=r1"));
		expect(viewer.token).toBeUndefined();
		expect(viewer.entry("abc")).toBe("/e/abc");
		expect(viewer.leaf("abc", "leaf")).toBe("/e/abc/b/leaf");
		expect(viewer.newRevision("abc")).toBe("/e/abc/revisions/new");
	});

	it("builds the edit link on the given origin", () => {
		expect(editLinkUrl("abc", "t/k+n", "https://expo.test")).toBe(
			"https://expo.test/e/abc?token=t%2Fk%2Bn",
		);
	});
});
