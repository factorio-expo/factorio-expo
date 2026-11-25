import { describe, expect, it } from "vitest";
import { aliasPath, sourceHost } from "./entry-source";

describe("sourceHost", () => {
	it("names the host of an https link", () => {
		expect(sourceHost("https://pastebin.com/aBcD1234")).toEqual({
			url: "https://pastebin.com/aBcD1234",
			host: "pastebin.com",
		});
	});

	it("shows nothing for an upload or a link that is not https", () => {
		expect(sourceHost(undefined)).toBeUndefined();
		expect(sourceHost("")).toBeUndefined();
		expect(sourceHost("javascript:alert(1)")).toBeUndefined();
		expect(sourceHost("http://pastebin.com/x")).toBeUndefined();
		expect(sourceHost("not a url")).toBeUndefined();
	});
});

describe("aliasPath", () => {
	it("is /@user/alias for an entry with one", () => {
		expect(aliasPath({ alias: "iron-smelter", owner_username: "alice" })).toBe(
			"/@alice/iron-smelter",
		);
	});

	it("is undefined without an alias or an owner", () => {
		expect(aliasPath({ owner_username: "alice" })).toBeUndefined();
		expect(aliasPath({ alias: "x-y-z" })).toBeUndefined();
	});
});
