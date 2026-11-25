import { describe, expect, it, vi } from "vitest";

import { nativeShare, redditSubmitUrl, shareableUrl } from "./share";

describe("shareableUrl", () => {
	it("drops the query string and fragment, so an edit token is never shared", () => {
		const page = new URL("https://expo.test/e/abc123/b/xyz?token=secret&utm=1#node");
		expect(shareableUrl(page)).toBe("https://expo.test/e/abc123/b/xyz");
	});

	it("uses the public origin, not the request's internal one", () => {
		const page = new URL("http://localhost:4321/e/abc123?token=secret");
		expect(shareableUrl(page, new URL("https://expo.test"))).toBe("https://expo.test/e/abc123");
	});
});

describe("redditSubmitUrl", () => {
	it("encodes the link and title in Reddit's submit form", () => {
		const url = new URL(redditSubmitUrl("https://expo.test/e/abc", "Gleba & more"));
		expect(url.origin + url.pathname).toBe("https://www.reddit.com/submit");
		expect(url.searchParams.get("url")).toBe("https://expo.test/e/abc");
		expect(url.searchParams.get("title")).toBe("Gleba & more");
	});
});

describe("nativeShare", () => {
	const data = { url: "https://expo.test/e/abc", title: "T" };

	it("is false without the Web Share API", async () => {
		expect(await nativeShare(data, {} as Navigator)).toBe(false);
	});

	it("is false when the data cannot be shared", async () => {
		const share = vi.fn<() => Promise<void>>();
		expect(await nativeShare(data, { share, canShare: () => false })).toBe(false);
		expect(share).not.toHaveBeenCalled();
	});

	it("is true once shared and false when the visitor closes the sheet", async () => {
		expect(
			await nativeShare(data, {
				share: vi.fn<() => Promise<void>>().mockResolvedValue(undefined),
			}),
		).toBe(true);
		const abort = new DOMException("closed", "AbortError");
		expect(
			await nativeShare(data, {
				share: vi.fn<() => Promise<void>>().mockRejectedValue(abort),
			}),
		).toBe(false);
	});
});
