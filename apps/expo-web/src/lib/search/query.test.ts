import { describe, expect, it } from "vitest";
import {
	DEFAULT_SORT,
	MAX_NUMERIC_FILTERS,
	PAGE_SIZE,
	hasAdvancedFilters,
	parseSearchParams,
	toApiQuery,
	canonicalFormParams,
	toQueryString,
} from "./query";

const parse = (query: string) => parseSearchParams(new URL(`http://x/?${query}`));

const production = (filter: object) =>
	parse(
		`production_filters=${encodeURIComponent(JSON.stringify([{ direction: "out", operator: ">", rate: 1, ...filter }]))}`,
	);

const cost = (filters: object[]) =>
	parse(`cost_filters=${encodeURIComponent(JSON.stringify(filters))}`);

/** A JSON filter parameter as the API gets it. */
const sent = (value: string | undefined) => JSON.parse(value ?? "[]") as unknown[];

/** A submitted form. */
const form = (entries: [string, string][]) => {
	const data = new FormData();
	for (const [name, value] of entries) data.append(name, value);
	return data;
};

describe("parseSearchParams", () => {
	it("applies defaults", () => {
		const search = parse("");
		expect(search).toMatchObject({
			q: "",
			blueprintType: "all",
			sort: DEFAULT_SORT,
			offset: 0,
			entityQuality: "",
			filterError: null,
		});
	});

	it("falls back on unknown sort and quality values", () => {
		const search = parse("sort=title:asc&entity_quality=shiny&recipe_quality=legendary");
		expect(search.sort).toBe(DEFAULT_SORT);
		expect(search.entityQuality).toBe("");
		expect(search.recipeQuality).toBe("legendary");
	});

	it("shows blueprints only unless the URL names kinds", () => {
		expect(parse("").kinds).toEqual(["blueprint"]);
		expect(parse("kind=planet").kinds).toEqual(["blueprint"]);
		expect(toApiQuery(parse("")).kind).toBe("blueprint");
		expect(toQueryString(parse("kind=blueprint"))).toBe("");
		expect(parse("kind=book&kind=blueprint").kinds).toEqual(["blueprint", "book"]);
	});

	it("snaps offsets to a page boundary", () => {
		expect(parse("offset=45").offset).toBe(2 * PAGE_SIZE);
		expect(parse("offset=-3").offset).toBe(0);
	});

	it("keeps known kinds and whole positive footprints", () => {
		const search = parse("kind=book&kind=planet&footprint_width=0&footprint_height=12");
		expect(search.kinds).toEqual(["book"]);
		expect(search.footprintWidth).toBeNull();
		expect(search.footprintHeight).toBe(12);
		expect(parse("footprint_width=2.5").footprintWidth).toBeNull();
	});

	it("reports malformed filters", () => {
		expect(parse("production_filters=%5Bnope").filterError).toMatch(/Production/);
		expect(parse('platform_filters=[{"metric":"x"}]').filterError).toMatch(/Platform/);
	});

	it("takes the per tile and per MW directions", () => {
		expect(
			production({ type: "item", product: "iron-plate", direction: "out_per_tile" })
				.filterError,
		).toBeNull();
		expect(
			production({ type: "item", product: "iron-plate", direction: "out_per_mw" })
				.filterError,
		).toBeNull();
		expect(
			production({ type: "item", product: "iron-plate", direction: "in_per_mw" }).filterError,
		).toMatch(/Production/);
	});

	it("reads build cost filters", () => {
		const valid = { type: "item", resource: "iron-ore", operator: "<", amount: 100 };
		expect(cost([valid]).costFilters).toEqual([valid]);
		expect(cost([{ ...valid, amount: -1 }]).filterError).toMatch(/Build cost/);
		expect(cost([{ ...valid, type: "signal" }]).filterError).toMatch(/Build cost/);
		expect(cost(Array.from({ length: 9 }, () => valid)).filterError).toMatch(/Build cost/);
	});

	it("shows spoilers unless asked to hide them", () => {
		expect(parse("").hideSpoilers).toBe(false);
		expect(toApiQuery(parse("")).spoiler).toBeUndefined();
		expect(parse("spoiler=hide").hideSpoilers).toBe(true);
		expect(toApiQuery(parse("spoiler=hide")).spoiler).toBe("exclude");
	});

	it("takes at most MAX_NUMERIC_FILTERS rows of each filter list", () => {
		const row = {
			type: "item",
			product: "iron-plate",
			direction: "out",
			operator: ">",
			rate: 1,
		};
		const rows = (count: number) =>
			parse(
				`production_filters=${encodeURIComponent(JSON.stringify(Array.from({ length: count }, () => row)))}`,
			);
		expect(rows(MAX_NUMERIC_FILTERS).productionFilters).toHaveLength(MAX_NUMERIC_FILTERS);
		expect(rows(MAX_NUMERIC_FILTERS + 1).filterError).toMatch(/Production/);
	});

	it("needs a material type and keeps quality off fluids", () => {
		expect(
			production({ type: "item", product: "iron-plate", quality: "rare" }).filterError,
		).toBeNull();
		expect(production({ type: "fluid", product: "water" }).filterError).toBeNull();
		expect(production({ product: "iron-plate" }).filterError).toMatch(/Production/);
		expect(
			production({ type: "fluid", product: "water", quality: "rare" }).filterError,
		).toMatch(/Production/);
	});
});

describe("hasAdvancedFilters", () => {
	it("counts filters, not the text, order or page", () => {
		expect(hasAdvancedFilters(parse("q=smelter&sort=entity_count:desc&offset=40"))).toBe(false);
		for (const query of [
			"tags=mining",
			"mods=vanilla",
			"entities=biolab",
			"entity_quality=rare",
			"recipes=iron-plate",
			"recipe_quality=rare",
			"game_version=v2.0",
			"kind=book",
			"footprint_width=10",
			"footprint_height=10",
			"blueprint_type=platforms",
			"spoiler=hide",
			`cost_filters=${encodeURIComponent('[{"type":"item","resource":"iron-ore","operator":"<","amount":1}]')}`,
		]) {
			expect([query, hasAdvancedFilters(parse(query))]).toEqual([query, true]);
		}
	});
});

describe("toApiQuery", () => {
	it("maps every field to its API parameter in wire form", () => {
		expect(
			toApiQuery(
				parse(
					"q=a&tags=t&mods=m&entities=e&entity_quality=rare&recipes=r&recipe_quality=epic" +
						"&game_version=v2.0&kind=book&footprint_width=4&blueprint_type=factories&sort=view_count:desc&offset=20&spoiler=hide",
				),
			),
		).toEqual({
			q: "a",
			tags: "t",
			mods: "m",
			entities: "e",
			entity_quality: "rare",
			recipes: "r",
			recipe_quality: "epic",
			game_version: "v2.0",
			kind: "book",
			footprint_width: 4,
			footprint_height: undefined,
			blueprint_type: "factories",
			sort: "view_count:desc",
			limit: PAGE_SIZE,
			offset: 20,
			production_filters: undefined,
			platform_filters: undefined,
			cost_filters: undefined,
			spoiler: "exclude",
			created_after: undefined,
			created_before: undefined,
			similar_to: undefined,
		});
	});

	it("leaves defaults out and joins lists", () => {
		const query = toApiQuery(parse("tags=a&tags=b&kind=book&kind=blueprint"));
		expect(query).toMatchObject({ tags: "a,b", kind: "blueprint,book" });
		expect(query.q).toBeUndefined();
		expect(query.blueprint_type).toBeUndefined();
		expect(query.game_version).toBeUndefined();
	});

	const filters = encodeURIComponent(
		'[{"type":"item","product":"iron-plate","direction":"out","operator":">","rate":5}]',
	);
	const platforms = encodeURIComponent('[{"metric":"weight_tons","operator":">","value":100}]');

	it("drops filters the blueprint type ignores", () => {
		const base = `production_filters=${filters}&platform_filters=${platforms}`;
		const factories = toApiQuery(parse(`${base}&blueprint_type=factories`));
		expect(factories.platform_filters).toBeUndefined();
		expect(sent(factories.production_filters)).toHaveLength(1);
		const platformsQuery = toApiQuery(parse(`${base}&blueprint_type=platforms`));
		expect(platformsQuery.production_filters).toBeUndefined();
		expect(sent(platformsQuery.platform_filters)).toHaveLength(1);
		const all = toApiQuery(parse(base));
		expect(sent(all.production_filters)).toHaveLength(1);
		expect(sent(all.platform_filters)).toHaveLength(1);
	});

	it("always asks for one page", () => {
		expect(toApiQuery(parse("offset=40"))).toMatchObject({ limit: PAGE_SIZE, offset: 40 });
	});
});

describe("URL round trip", () => {
	it.each([
		"",
		"q=smelter",
		"q=a&tags=early-game&tags=mining&entities=assembling-machine-2&entity_quality=rare",
		"mods=vanilla&mods=krastorio-2",
		"recipes=iron-plate&recipe_quality=epic&game_version=2.0.0&sort=entity_count:desc&offset=20",
		"blueprint_type=platforms",
		"kind=book&kind=upgrade_planner&footprint_width=32&footprint_height=16",
		"spoiler=hide",
		`cost_filters=${encodeURIComponent('[{"type":"fluid","resource":"crude-oil","operator":"=","amount":0}]')}`,
	])("parse then serialize is stable: %s", (query) => {
		const once = toQueryString(parse(query));
		expect(toQueryString(parse(once))).toBe(once);
		expect(parse(once)).toEqual(parse(query));
	});

	it("omits defaults", () => {
		expect(toQueryString(parse("blueprint_type=all&sort=updated_at:desc&offset=0"))).toBe("");
	});
});

describe("canonicalFormParams", () => {
	it("leaves every default of a submitted form out", () => {
		const submitted = form([
			["q", ""],
			["blueprint_type", "all"],
			["kind", "blueprint"],
			["game_version", ""],
			["entity_quality", ""],
			["production_filters", "[]"],
			["platform_filters", "[]"],
			["cost_filters", "[]"],
			["sort", "updated_at:desc"],
		]);
		expect(canonicalFormParams(submitted).toString()).toBe("");
	});

	it("keeps what the visitor set, in canonical order", () => {
		const submitted = form([
			["sort", "view_count:desc"],
			["q", "smelter"],
			["kind", "book"],
			["tags", "mining"],
		]);
		expect(canonicalFormParams(submitted).toString()).toBe(
			"q=smelter&tags=mining&kind=book&sort=view_count%3Adesc",
		);
	});
});

describe("upload date range", () => {
	it("keeps real dates only", () => {
		const search = parse("created_after=2026-11-01&created_before=2026-02-31");
		expect(search.createdAfter).toBe("2026-11-01");
		expect(search.createdBefore).toBe("");
		expect(parse("created_after=yesterday").createdAfter).toBe("");
	});

	it("round-trips through the URL and the API query, and counts as an advanced filter", () => {
		const search = parse("created_before=2026-11-14&created_after=2026-11-01");
		expect(toQueryString(search)).toBe("created_after=2026-11-01&created_before=2026-11-14");
		expect(toApiQuery(search)).toMatchObject({
			created_after: "2026-11-01",
			created_before: "2026-11-14",
		});
		expect(hasAdvancedFilters(search)).toBe(true);
		expect(toApiQuery(parse(""))).toMatchObject({
			created_after: undefined,
			created_before: undefined,
		});
	});
});

describe("similar_to", () => {
	it("keeps a blueprint id only", () => {
		expect(parse("similar_to=abcdefghijkl").similarTo).toBe("abcdefghijkl");
		expect(parse("similar_to=nope").similarTo).toBe("");
		expect(toQueryString(parse("similar_to=abcdefghijkl&q=gears"))).toBe(
			"q=gears&similar_to=abcdefghijkl",
		);
		expect(toApiQuery(parse("similar_to=abcdefghijkl")).similar_to).toBe("abcdefghijkl");
	});
});
