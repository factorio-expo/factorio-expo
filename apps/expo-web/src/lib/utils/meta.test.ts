import { describe, expect, it } from "vitest";
import type { Node, Preview } from "@lib/api";
import { coverPreview, creativeWorkJsonLd, jsonLd, metaDescription } from "./meta";

describe("metaDescription", () => {
	it("strips Markdown and icon tokens", () => {
		expect(metaDescription("## Smelting\n**Fast** [item=iron-plate] array", "x")).toBe(
			"Smelting Fast array",
		);
	});

	it("falls back when empty", () => {
		expect(metaDescription("  ", "fallback")).toBe("fallback");
		expect(metaDescription(undefined, "fallback")).toBe("fallback");
	});

	it("cuts long text at a word", () => {
		const result = metaDescription("word ".repeat(60), "x");
		expect(result.length).toBeLessThanOrEqual(160);
		expect(result.endsWith("word…")).toBe(true);
	});
});

describe("jsonLd", () => {
	it("cannot close the script tag", () => {
		const json = jsonLd({ name: "</script><b>" });
		expect(json).not.toContain("<");
		expect(JSON.parse(json)).toEqual({ name: "</script><b>" });
	});
});

describe("creativeWorkJsonLd", () => {
	const site = new URL("https://example.org");
	const entry = {
		created_at: "2026-10-01T00:00:00Z",
		updated_at: "2026-10-02T00:00:00Z",
		owner_username: "ann",
	};

	it("builds absolute URLs, the author and what it is part of", () => {
		const data = JSON.parse(
			creativeWorkJsonLd({
				site,
				path: "/e/abc/b/def",
				name: "Smelter",
				description: "Smelts.",
				entry,
				image: "/api/v1/blueprints/def/thumbnail.webp",
				isPartOf: { type: "CreativeWork", name: "Book", path: "/e/abc" },
			}),
		);
		expect(data).toEqual({
			"@context": "https://schema.org",
			"@type": "CreativeWork",
			name: "Smelter",
			description: "Smelts.",
			url: "https://example.org/e/abc/b/def",
			dateCreated: entry.created_at,
			dateModified: entry.updated_at,
			image: "https://example.org/api/v1/blueprints/def/thumbnail.webp",
			author: { "@type": "Person", name: "ann", url: "https://example.org/u/ann" },
			isPartOf: {
				"@type": "CreativeWork",
				name: "Book",
				url: "https://example.org/e/abc",
			},
		});
	});

	it("adds keywords and views only when given, and no author for anonymous entries", () => {
		const data = JSON.parse(
			creativeWorkJsonLd({
				site,
				path: "/e/abc",
				name: "Smelter",
				description: "Smelts.",
				entry: { ...entry, owner_username: undefined },
				keywords: ["Smelting", "Early game"],
				viewCount: 7,
				isPartOf: { type: "WebSite", name: "Factorio Expo", path: "/" },
			}),
		);
		expect(data.author).toBeUndefined();
		expect(data.image).toBeUndefined();
		expect(data.keywords).toBe("Smelting, Early game");
		expect(data.interactionStatistic.userInteractionCount).toBe(7);
		expect(data.isPartOf).toEqual({
			"@type": "WebSite",
			name: "Factorio Expo",
			url: "https://example.org/",
		});
	});
});

const preview = (id: string) =>
	({ thumbnail_url: `/api/v1/blueprints/${id}/thumbnail.webp` }) as Preview;
const leaf = (id: string, rendered: boolean): Node => ({
	kind: "blueprint",
	id,
	label: id,
	description: "",
	icons: "",
	...(rendered && { preview: preview(id) }),
});
const book = (id: string, children: Node[]): Node => ({
	kind: "book",
	id,
	label: id,
	description: "",
	icons: "",
	children,
});

describe("coverPreview", () => {
	it("takes the first rendered leaf depth-first", () => {
		const tree = book("root", [
			leaf("a", false),
			book("inner", [leaf("b", false), leaf("c", true)]),
			leaf("d", true),
		]);
		expect(coverPreview(tree)?.thumbnail_url).toBe("/api/v1/blueprints/c/thumbnail.webp");
	});

	it("is undefined without a rendered leaf", () => {
		expect(coverPreview(book("root", [leaf("a", false), book("empty", [])]))).toBeUndefined();
		expect(coverPreview(undefined)).toBeUndefined();
	});
});
