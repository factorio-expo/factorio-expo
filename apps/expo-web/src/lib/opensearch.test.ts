import { describe, expect, it } from "vitest";
import { openSearchDescription } from "./opensearch";

describe("openSearchDescription", () => {
	it("points the search template at the browse page's query", () => {
		const xml = openSearchDescription("https://expo.example");
		expect(xml).toContain("<ShortName>Factorio Expo</ShortName>");
		expect(xml).toContain('template="https://expo.example/browse?q={searchTerms}"');
		expect(xml).toContain('rel="self" template="https://expo.example/opensearch.xml"');
		expect(xml).toContain(">https://expo.example/favicon.ico</Image>");
	});
});
