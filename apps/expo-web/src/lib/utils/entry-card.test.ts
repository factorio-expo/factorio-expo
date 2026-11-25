import { describe, expect, it } from "vitest";

import type { EntrySummary, SearchResultCard } from "@lib/api";
import { cardView, toCard, type EntryCardData } from "./entry-card";

const lookups = {
	tags: (slugs: string[]) => slugs.map((slug) => ({ slug, name: slug.toUpperCase(), icon: "" })),
	modBadges: (values: string[]) =>
		values.map((value) => ({ value, label: value, href: `/browse?mods=${value}` })),
};

const result = {
	entry_id: "abc",
	node_id: "n1",
	book_title: "Book",
	title: "",
	description: "d",
	cover_urls: ["/c.png"],
	kind: "blueprint",
	icons: "",
	tags: ["oil"],
	mod_badges: ["space-age"],
	favorites_count: 1,
	view_count: 2,
	is_favorited: true,
	spoiler: false,
	game_version: "2.1.0",
	compat_21: "compatible",
	records: [{ board: "fastest", group: "ships", title: "Fastest ship", blueprint_id: "n1" }],
} as unknown as SearchResultCard;

const summary = {
	id: "xyz",
	title: "",
	label: "Label",
	description: "",
	kind: "book",
	icons: "",
	favorites_count: 0,
	view_count: 0,
	is_favorited: false,
	is_pinned: true,
	spoiler: true,
	game_version: "2.0.0",
	compat_21: "",
	records: [],
} as unknown as EntrySummary;

describe("toCard", () => {
	it("adapts a search result, resolving tags and badges", () => {
		const card = toCard(result, lookups);
		expect(card).toMatchObject({
			entry_id: "abc",
			node_id: "n1",
			book_title: "Book",
			title: "Untitled blueprint",
			cover_urls: ["/c.png"],
			compat_21: "compatible",
			game_version: "2.1.0",
			records: [
				{ board: "fastest", group: "ships", title: "Fastest ship", blueprint_id: "n1" },
			],
		});
		expect(card.tags).toEqual([{ slug: "oil", name: "OIL", icon: "" }]);
		expect(card.mod_badges?.[0]?.value).toBe("space-age");
	});

	it("adapts a listing row, falling back to its label", () => {
		const card = toCard(summary);
		expect(card).toMatchObject({
			entry_id: "xyz",
			title: "Label",
			is_pinned: true,
			spoiler: true,
			records: [],
		});
		expect(card.tags).toBeUndefined();
	});
});

/** An API path as the card's public URL. */
const asset = (path: string) => `https://api.test${path}`;

describe("cardView", () => {
	const base = toCard(summary);

	it("opens the entry, with the cover as a public URL", () => {
		const view = cardView({ ...base, preview_url: "/p.webp" }, asset);
		expect(view).toMatchObject({
			href: "/e/xyz",
			bookHref: undefined,
			previewUrl: "https://api.test/p.webp",
			mosaic: [],
			moreRecords: 0,
			recordTitle: "",
		});
	});

	it("opens a book's blueprint page or a planner's row, and links the book", () => {
		const inBook = { ...base, book_title: "Book", node_id: "n1" };
		expect(cardView({ ...inBook, kind: "blueprint" }, asset)).toMatchObject({
			href: "/e/xyz/b/n1",
			bookHref: "/e/xyz",
		});
		expect(cardView({ ...inBook, kind: "upgrade_planner" }, asset).href).toBe("/e/xyz#node-n1");
	});

	it("shows a mosaic only from two covers on", () => {
		const one = cardView({ ...base, preview_url: "/p", cover_urls: ["/a"] }, asset);
		expect(one.mosaic).toEqual([]);
		const two = cardView({ ...base, preview_url: "/p", cover_urls: ["/a", "/b"] }, asset);
		expect(two.mosaic).toEqual(["https://api.test/a", "https://api.test/b"]);
		expect(cardView({ ...base, cover_urls: ["/a", "/b"] }, asset).mosaic).toEqual([]);
	});

	it("names every record and counts the extra ones", () => {
		const records = [
			{ board: "fastest", group: "ships", title: "Fastest ship", blueprint_id: "n1" },
			{ board: "heaviest", group: "ships", title: "Heaviest ship", blueprint_id: "n1" },
		] as unknown as EntryCardData["records"];
		const view = cardView({ ...base, records }, asset);
		expect(view.record?.title).toBe("Fastest ship");
		expect(view.moreRecords).toBe(1);
		expect(view.recordTitle).toBe(
			"Holds the Fastest ship record; Holds the Heaviest ship record",
		);
	});

	it("carries the version badge", () => {
		expect(cardView({ ...base, game_version: "2.1.0" }, asset).badge?.title).toBe(
			"Made in Factorio 2.1",
		);
	});
});
