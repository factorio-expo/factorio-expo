// Translation between the browse page's URL, typed search state and the API
// query, including defaults and validation. Pages and components use this
// module instead of knowing the defaults themselves. One descriptor per
// filter (`fields`) says how it reads the URL, writes it back, becomes the
// API query and counts as an advanced filter, the way Go's search.filters
// table drives search.ParseQuery.
import { QUALITIES } from "@factorio-expo/ui/game/icon-token.ts";
import { isIsoDate } from "@factorio-expo/ui/utils/dates.ts";
import type {
	NodeKind,
	ProductionDirection,
	SearchCostFilter,
	SearchPlatformFilter,
	SearchProductionFilter,
	SearchQuery,
	SearchSort,
} from "@lib/api";

export type BlueprintTypeFilter = "all" | "factories" | "platforms";

export const PAGE_SIZE = 20;
export const DEFAULT_SORT: SearchSort = "updated_at:desc";

export const sortOptions = [
	{ value: "relevance", label: "Relevance" },
	{ value: "created_at:desc", label: "Upload date: newest first" },
	{ value: "created_at:asc", label: "Upload date: oldest first" },
	{ value: "updated_at:desc", label: "Update date: newest first" },
	{ value: "updated_at:asc", label: "Update date: oldest first" },
	{ value: "entity_count:desc", label: "Entity count: most first" },
	{ value: "entity_count:asc", label: "Entity count: fewest first" },
	{ value: "leaf_count:desc", label: "Blueprint count: most first" },
	{ value: "leaf_count:asc", label: "Blueprint count: fewest first" },
	{ value: "view_count:desc", label: "Views: most first" },
	{ value: "view_count:asc", label: "Views: fewest first" },
] satisfies { value: SearchSort; label: string }[];

/** The mod filter's own options, ahead of the modsets from GET /api/v1/modsets
 * (Go mods.Vanilla and mods.SpaceAge). */
export const MOD_FILTER_OPTIONS = [
	{ value: "vanilla", label: "Vanilla only" },
	{ value: "space-age", label: "Space Age" },
];

/** The kind filter's options: what a result is. */
export const KIND_FILTER_OPTIONS: { value: NodeKind; label: string }[] = [
	{ value: "blueprint", label: "Blueprint" },
	{ value: "book", label: "Blueprint book" },
	{ value: "upgrade_planner", label: "Upgrade planner" },
	{ value: "deconstruction_planner", label: "Deconstruction planner" },
];

/**
 * The kinds the browse page shows when the URL names none: blueprints only,
 * since a book and its planners next to their own blueprints crowd the list.
 * (The API's default is every kind.)
 */
export const DEFAULT_KINDS: NodeKind[] = ["blueprint"];

/** The largest footprint bound the form takes, in tiles. */
export const MAX_FOOTPRINT = 100000;

/** The production filter's direction choices (Go search.ValidDirection). */
export const PRODUCTION_DIRECTIONS: { value: ProductionDirection; label: string }[] = [
	{ value: "out", label: "Out" },
	{ value: "in", label: "In" },
	{ value: "out_per_tile", label: "Out per tile" },
	{ value: "out_per_mw", label: "Out per MW" },
];

/** The most rows of each numeric filter list the API takes (Go search.maxNumericFilters). */
export const MAX_NUMERIC_FILTERS = 8;

export interface SearchParams {
	q: string;
	tags: string[];
	/** Mod filter values: "vanilla", "space-age" or modset names. */
	mods: string[];
	entities: string[];
	entityQuality: string;
	recipes: string[];
	recipeQuality: string;
	gameVersion: string;
	/** Result kinds, OR-matched; DEFAULT_KINDS when the URL names none. */
	kinds: NodeKind[];
	/** "Fits in" bounds in tiles, either way round; null when unset. */
	footprintWidth: number | null;
	footprintHeight: number | null;
	blueprintType: BlueprintTypeFilter;
	sort: SearchSort;
	offset: number;
	productionFilters: SearchProductionFilter[];
	platformFilters: SearchPlatformFilter[];
	costFilters: SearchCostFilter[];
	/** Show entries marked spoiler/NSFW (their previews stay hidden until a
	 * click); left out by default. */
	hideSpoilers: boolean;
	/** Created on or after this date (`2026-11-14`, UTC); empty when unset. */
	createdAfter: string;
	/** Created on or before this date (that whole UTC day); empty when unset. */
	createdBefore: string;
	/** A blueprint's id: list the blueprints like it (the API's similar_to); empty when unset. */
	similarTo: string;
	filterError: string | null;
}

/** A blueprint id: 12 letters and digits (Go shortid.Valid). */
/** A list parameter: comma-separated, absent when empty. */
const commaList = (values: readonly string[]) => (values.length > 0 ? values.join(",") : undefined);

const isBlueprintId = (value: string) => /^[0-9A-Za-z]{12}$/.test(value);

const isComparison = (operator: unknown, value: unknown) =>
	(operator === ">" || operator === "=" || operator === "<") &&
	typeof value === "number" &&
	Number.isFinite(value) &&
	value >= 0;

function isProductionFilter(value: unknown): value is SearchProductionFilter {
	if (typeof value !== "object" || value === null) return false;
	if (
		!("type" in value) ||
		!("product" in value) ||
		!("direction" in value) ||
		!("operator" in value) ||
		!("rate" in value)
	) {
		return false;
	}

	const quality = "quality" in value ? value.quality : undefined;
	return (
		(value.type === "item" || value.type === "fluid") &&
		typeof value.product === "string" &&
		(quality === undefined ||
			(value.type === "item" &&
				typeof quality === "string" &&
				(QUALITIES as readonly string[]).includes(quality))) &&
		PRODUCTION_DIRECTIONS.some((direction) => direction.value === value.direction) &&
		isComparison(value.operator, value.rate)
	);
}

function isPlatformFilter(value: unknown): value is SearchPlatformFilter {
	if (typeof value !== "object" || value === null) return false;
	if (!("metric" in value) || !("operator" in value) || !("value" in value)) return false;
	return (
		(value.metric === "weight_tons" ||
			value.metric === "thrust_mn" ||
			value.metric === "max_speed_km_s") &&
		isComparison(value.operator, value.value)
	);
}

function isCostFilter(value: unknown): value is SearchCostFilter {
	if (typeof value !== "object" || value === null) return false;
	if (
		!("type" in value) ||
		!("resource" in value) ||
		!("operator" in value) ||
		!("amount" in value)
	) {
		return false;
	}
	return (
		(value.type === "item" || value.type === "fluid") &&
		typeof value.resource === "string" &&
		value.resource.trim() !== "" &&
		isComparison(value.operator, value.amount)
	);
}

function isDefaultKinds(kinds: NodeKind[]): boolean {
	return (
		kinds.length === DEFAULT_KINDS.length && kinds.every((kind) => DEFAULT_KINDS.includes(kind))
	);
}

/** The state of a URL that names nothing. */
function defaults(): SearchParams {
	return {
		q: "",
		tags: [],
		mods: [],
		entities: [],
		entityQuality: "",
		recipes: [],
		recipeQuality: "",
		gameVersion: "",
		kinds: [...DEFAULT_KINDS],
		footprintWidth: null,
		footprintHeight: null,
		blueprintType: "all",
		sort: DEFAULT_SORT,
		offset: 0,
		productionFilters: [],
		platformFilters: [],
		costFilters: [],
		hideSpoilers: false,
		createdAfter: "",
		createdBefore: "",
		similarTo: "",
		filterError: null,
	};
}

/** One search filter (or the text, order or page) as the browse page handles it. */
interface SearchField {
	/** URL -> state; an invalid value keeps the default (a filter list says why in filterError). */
	read(url: URLSearchParams, state: SearchParams): void;
	/** State -> canonical URL, defaults omitted. */
	write(state: SearchParams, url: URLSearchParams): void;
	/** State -> its part of the API query, in wire form; a default is left out. */
	api(state: SearchParams): SearchQuery;
	/** Whether it is set away from its default; absent for the text, order and page. */
	advanced?(state: SearchParams): boolean;
}

type ListKey = "tags" | "mods" | "entities" | "recipes";

/** A repeated URL parameter, sent as the API's list of the same name. */
const listField = (key: ListKey): SearchField => ({
	read: (url, state) => {
		state[key] = url.getAll(key);
	},
	write: (state, url) => {
		for (const value of state[key]) url.append(key, value);
	},
	api: (state) => ({ [key]: commaList(state[key]) }),
	advanced: (state) => state[key].length > 0,
});

/** The quality tier of the entity or recipe filter; unknown tiers are dropped. */
const qualityField = (
	key: "entityQuality" | "recipeQuality",
	param: "entity_quality" | "recipe_quality",
): SearchField => ({
	read: (url, state) => {
		const value = url.get(param);
		state[key] = value && (QUALITIES as readonly string[]).includes(value) ? value : "";
	},
	write: (state, url) => {
		if (state[key]) url.set(param, state[key]);
	},
	api: (state) => ({ [param]: state[key] || undefined }),
	advanced: (state) => Boolean(state[key]),
});

/** A footprint bound: a whole number of tiles, at most MAX_FOOTPRINT. */
const footprintField = (
	key: "footprintWidth" | "footprintHeight",
	param: "footprint_width" | "footprint_height",
): SearchField => ({
	read: (url, state) => {
		const value = url.get(param);
		const size = Number(value);
		state[key] =
			value && Number.isInteger(size) && size > 0 && size <= MAX_FOOTPRINT ? size : null;
	},
	write: (state, url) => {
		const size = state[key];
		if (size !== null) url.set(param, String(size));
	},
	api: (state) => ({ [param]: state[key] ?? undefined }),
	advanced: (state) => state[key] !== null,
});

/** A date bound of the upload date range: a real `YYYY-MM-DD` date; anything else is dropped. */
const dateField = (
	key: "createdAfter" | "createdBefore",
	param: "created_after" | "created_before",
): SearchField => ({
	read: (url, state) => {
		const value = url.get(param) ?? "";
		state[key] = isIsoDate(value) ? value : "";
	},
	write: (state, url) => {
		if (state[key]) url.set(param, state[key]);
	},
	api: (state) => ({ [param]: state[key] || undefined }),
	advanced: (state) => Boolean(state[key]),
});

/** A JSON array of at most MAX_NUMERIC_FILTERS numeric filters, each checked by `valid`. */
function filterList<T>(options: {
	param: "production_filters" | "platform_filters" | "cost_filters";
	/** filterError when the URL's list cannot be read. */
	invalid: string;
	valid: (value: unknown) => value is T;
	get: (state: SearchParams) => T[];
	set: (state: SearchParams, filters: T[]) => void;
	/** The filters the API gets (the blueprint type may rule them out). */
	sent?: (state: SearchParams) => T[];
}): SearchField {
	const { param, invalid, valid, get, set } = options;
	return {
		read: (url, state) => {
			const raw = url.get(param);
			if (!raw) return;
			let parsed: unknown;
			try {
				parsed = JSON.parse(raw);
			} catch {
				state.filterError = invalid;
				return;
			}
			if (
				Array.isArray(parsed) &&
				parsed.length <= MAX_NUMERIC_FILTERS &&
				parsed.every(valid)
			) {
				set(state, parsed);
			} else {
				state.filterError = invalid;
			}
		},
		write: (state, url) => {
			if (get(state).length > 0) url.set(param, JSON.stringify(get(state)));
		},
		api: (state) => {
			const sent = (options.sent ?? get)(state);
			return { [param]: sent.length > 0 ? JSON.stringify(sent) : undefined };
		},
		advanced: (state) => get(state).length > 0,
	};
}

/** Every field, in canonical URL order. */
const fields: SearchField[] = [
	{
		read: (url, state) => {
			state.q = url.get("q") ?? "";
		},
		write: (state, url) => {
			if (state.q) url.set("q", state.q);
		},
		api: (state) => ({ q: state.q || undefined }),
	},
	listField("tags"),
	listField("mods"),
	listField("entities"),
	qualityField("entityQuality", "entity_quality"),
	listField("recipes"),
	qualityField("recipeQuality", "recipe_quality"),
	{
		read: (url, state) => {
			state.gameVersion = url.get("game_version") ?? "";
		},
		write: (state, url) => {
			if (state.gameVersion) url.set("game_version", state.gameVersion);
		},
		api: (state) => ({ game_version: state.gameVersion || undefined }),
		advanced: (state) => Boolean(state.gameVersion),
	},
	{
		// Known kinds in option order; DEFAULT_KINDS when the URL names none.
		read: (url, state) => {
			const requested = url.getAll("kind");
			const kinds = KIND_FILTER_OPTIONS.map((option) => option.value).filter((kind) =>
				requested.includes(kind),
			);
			state.kinds = kinds.length > 0 ? kinds : [...DEFAULT_KINDS];
		},
		write: (state, url) => {
			if (!isDefaultKinds(state.kinds))
				for (const kind of state.kinds) url.append("kind", kind);
		},
		api: (state) => ({ kind: commaList(state.kinds) }),
		advanced: (state) => !isDefaultKinds(state.kinds),
	},
	footprintField("footprintWidth", "footprint_width"),
	footprintField("footprintHeight", "footprint_height"),
	{
		read: (url, state) => {
			const requested = url.get("blueprint_type");
			state.blueprintType =
				requested === "factories" || requested === "platforms" ? requested : "all";
		},
		write: (state, url) => {
			if (state.blueprintType !== "all") url.set("blueprint_type", state.blueprintType);
		},
		api: (state) => ({
			blueprint_type: state.blueprintType === "all" ? undefined : state.blueprintType,
		}),
		advanced: (state) => state.blueprintType !== "all",
	},
	{
		read: (url, state) => {
			const requested = url.get("sort");
			state.sort =
				sortOptions.find((option) => option.value === requested)?.value ?? DEFAULT_SORT;
		},
		write: (state, url) => {
			if (state.sort !== DEFAULT_SORT) url.set("sort", state.sort);
		},
		api: (state) => ({ sort: state.sort }),
	},
	{
		// Snapped to a page boundary; the API always gets one page.
		read: (url, state) => {
			const requested = Number.parseInt(url.get("offset") ?? "0", 10);
			state.offset =
				Number.isFinite(requested) && requested > 0
					? Math.floor(requested / PAGE_SIZE) * PAGE_SIZE
					: 0;
		},
		write: (state, url) => {
			if (state.offset > 0) url.set("offset", String(state.offset));
		},
		api: (state) => ({ limit: PAGE_SIZE, offset: state.offset }),
	},
	// The blueprint type rules out production or platform filters (the API
	// drops them too, Go search.filters).
	filterList({
		param: "production_filters",
		invalid: "Production filters could not be loaded.",
		valid: isProductionFilter,
		get: (state) => state.productionFilters,
		set: (state, filters) => {
			state.productionFilters = filters;
		},
		sent: (state) => (state.blueprintType === "platforms" ? [] : state.productionFilters),
	}),
	filterList({
		param: "platform_filters",
		invalid: "Platform filters could not be loaded.",
		valid: isPlatformFilter,
		get: (state) => state.platformFilters,
		set: (state, filters) => {
			state.platformFilters = filters;
		},
		sent: (state) => (state.blueprintType === "factories" ? [] : state.platformFilters),
	}),
	filterList({
		param: "cost_filters",
		invalid: "Build cost filters could not be loaded.",
		valid: isCostFilter,
		get: (state) => state.costFilters,
		set: (state, filters) => {
			state.costFilters = filters;
		},
	}),
	{
		read: (url, state) => {
			state.hideSpoilers = url.get("spoiler") === "hide";
		},
		write: (state, url) => {
			if (state.hideSpoilers) url.set("spoiler", "hide");
		},
		api: (state) => ({ spoiler: state.hideSpoilers ? "exclude" : undefined }),
		advanced: (state) => state.hideSpoilers,
	},
	dateField("createdAfter", "created_after"),
	dateField("createdBefore", "created_before"),
	{
		read: (url, state) => {
			const value = url.get("similar_to") ?? "";
			state.similarTo = isBlueprintId(value) ? value : "";
		},
		write: (state, url) => {
			if (state.similarTo) url.set("similar_to", state.similarTo);
		},
		api: (state) => ({ similar_to: state.similarTo || undefined }),
	},
];

/** URL -> typed state. Unknown sort/quality values fall back to their defaults. */
export function parseSearchParams(url: URL): SearchParams {
	const state = defaults();
	for (const field of fields) field.read(url.searchParams, state);
	return state;
}

/** Whether any filter of the advanced panel is set. */
export function hasAdvancedFilters(search: SearchParams): boolean {
	return fields.some((field) => field.advanced?.(search) ?? false);
}

/** Typed state -> the API's query parameters, in wire form. */
export function toApiQuery(search: SearchParams): SearchQuery {
	const query: SearchQuery = {};
	for (const field of fields) Object.assign(query, field.api(search));
	return query;
}

/** Typed state -> canonical URL query string (defaults omitted). */
export function toQueryString(search: SearchParams): string {
	return canonicalParams(search).toString();
}

/** Typed state -> canonical URL parameters (defaults omitted). */
export function canonicalParams(search: SearchParams): URLSearchParams {
	const params = new URLSearchParams();
	for (const field of fields) field.write(search, params);
	return params;
}

/**
 * The canonical form of a submitted search form: read like a URL, so every
 * default (no text, any version, every kind, an empty filter list) and
 * every invalid value is left out.
 */
export function canonicalFormParams(form: FormData): URLSearchParams {
	const url = new URL("http://search.invalid/");
	for (const [name, value] of form)
		if (typeof value === "string") url.searchParams.append(name, value);
	return canonicalParams(parseSearchParams(url));
}
