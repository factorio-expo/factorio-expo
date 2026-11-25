// Exchange strings for tests, built from the shared fixtures in test-data/.
// Tests relabel them so every upload is recognisable on screen.
import { randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { deflateSync, inflateSync } from "node:zlib";

const testData = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../test-data");
const read = (file: string) => readFileSync(path.join(testData, file), "utf8").trim();

/** A single 2.1 blueprint ("Starter smelter", 38 entities). */
export const BLUEPRINT = read("bp-starter-smelter.txt");
/** A single 2.1 blueprint ("Parametric assembler row": 6 assembling machine 3s, 6 parameters). */
export const BLUEPRINT_21 = read("bp-parametric-assembler-row.txt");
/** A different single 2.1 blueprint ("Red science 5+1"), used as a second revision. */
export const OTHER_BLUEPRINT = read("bp-science-clean.txt");
/** A nested 2.1 book ("Parametric crafts"): "Assemblers" and "Fluids" sub-books plus loose blueprints. */
export const BOOK = read("book-parametric-crafts.txt");
/** A space platform (test-data/ship-hauler.txt, about 1,800 entities): a job long enough to crash Factorio during it. */
export const PLATFORM = read("ship-hauler.txt");
/** A book of 12 deconstruction planners ("Deconstruction set"), the first "Cliffs". */
export const DECONSTRUCTION_BOOK = read("planner-deconstruct-set.txt");
/** A book of upgrade planners and sub-books ("Upgrade set"). */
export const UPGRADE_BOOK = read("planner-upgrade-set.txt");
/**
 * A Factorio 2.0 blueprint with Space Age content: a recycler (the Recycler mod)
 * and a biochamber breeding fish, plus logistic chests and a roboport (the
 * "Bot network" tag). For what the default blueprint, a vanilla 2.1 one, lacks.
 */
export const SPACE_AGE_20 = encode({
	blueprint: {
		label: "Fish and recyclers",
		item: "blueprint",
		version: 562949957025792, // 2.0.55
		entities: [
			{ entity_number: 1, name: "recycler", position: { x: 1, y: 2 } },
			{
				entity_number: 2,
				name: "biochamber",
				position: { x: 6.5, y: 1.5 },
				recipe: "fish-breeding",
			},
			{ entity_number: 3, name: "roboport", position: { x: 12, y: 2 } },
			{ entity_number: 4, name: "passive-provider-chest", position: { x: 15.5, y: 0.5 } },
			{ entity_number: 5, name: "requester-chest", position: { x: 15.5, y: 2.5 } },
		],
	},
});

type Entity = {
	entity_number?: number;
	name?: string;
	position?: { x: number; y: number };
	recipe?: string;
	tags?: Record<string, string>;
};

type Node = {
	blueprint?: {
		label?: string;
		description?: string;
		entities?: Entity[];
		item?: string;
		version?: number;
		"snap-to-grid"?: { x: number; y: number };
		"absolute-snapping"?: boolean;
		"position-relative-to-grid"?: { x: number; y: number };
	};
	blueprint_book?: {
		label?: string;
		blueprints?: Node[];
		item?: string;
		active_index?: number;
		version?: number;
	};
	deconstruction_planner?: { label?: string; settings?: Record<string, unknown> };
	index?: number;
};

export function decode(exchange: string): Node {
	return JSON.parse(inflateSync(Buffer.from(exchange.slice(1), "base64")).toString("utf8"));
}

export function encode(node: Node): string {
	return `0${deflateSync(Buffer.from(JSON.stringify(node), "utf8"), { level: 9 }).toString("base64")}`;
}

/** The same blueprint or book with a different root label. */
export function relabel(exchange: string, label: string): string {
	const node = decode(exchange);
	const root = node.blueprint ?? node.blueprint_book;
	if (!root) throw new Error("not a blueprint or book");
	root.label = label;
	return encode(node);
}

/** A small blueprint of the named entities, one per 10 tiles in a row. */
export function blueprintOf(label: string, names: string[]): string {
	const entities = names.map((name, index) => ({
		entity_number: index + 1,
		name,
		position: { x: index * 10 + 0.5, y: 0.5 },
	}));
	return encode({ blueprint: { label, entities, item: "blueprint", version: 562949958139904 } });
}

/** A book of the given blueprint strings, in order. */
export function bookOf(label: string, blueprints: string[]): string {
	return encode({
		blueprint_book: {
			label,
			blueprints: blueprints.map((exchange, index) => ({ ...decode(exchange), index })),
			item: "blueprint-book",
			active_index: 0,
			version: 562949958139904,
		},
	});
}

/** The book's first planner whose label matches, as a string of its own. */
export function plannerFrom(book: string, label: string): string {
	const child = decode(book).blueprint_book?.blueprints?.find(
		(node) => node.deconstruction_planner?.label === label,
	);
	if (!child) throw new Error(`no planner "${label}"`);
	return encode({ deconstruction_planner: child.deconstruction_planner });
}

/** The same planner with a different label. */
export function relabelPlanner(planner: string, label: string): string {
	const node = decode(planner);
	if (!node.deconstruction_planner) throw new Error("not a deconstruction planner");
	node.deconstruction_planner.label = label;
	return encode(node);
}

/** The blueprint snapping to an absolute grid of the given size and offset. */
export function withAbsoluteGrid(
	exchange: string,
	grid: { x: number; y: number },
	offset: { x: number; y: number },
): string {
	const node = decode(exchange);
	if (!node.blueprint) throw new Error("not a blueprint");
	node.blueprint["snap-to-grid"] = grid;
	node.blueprint["absolute-snapping"] = true;
	node.blueprint["position-relative-to-grid"] = offset;
	return encode(node);
}

/** The blueprint plus one entity no listed mod defines (mod detection's
 * "unknown mod"), placed clear of the others. */
export function withEntity(exchange: string, name: string): string {
	const node = decode(exchange);
	const entities = node.blueprint?.entities;
	if (!entities) throw new Error("not a blueprint with entities");
	const numbers = entities.map((entity) => entity.entity_number ?? 0);
	entities.push({
		entity_number: Math.max(...numbers) + 1,
		name,
		position: { x: 500.5, y: 500.5 },
	});
	return encode(node);
}

/**
 * The blueprint with ~2.6 MB of incompressible mod data in an entity's tags:
 * the API accepts it (8 MiB uploads) but the render worker refuses a job that
 * big (RUNNER_MAX_REQUEST_BYTES, 2 MiB), so both phases fail for good with
 * `request_too_large`. Entity tags are not shown on the page.
 */
export function oversized(exchange: string): string {
	const node = decode(exchange);
	const entity = node.blueprint?.entities?.[0];
	if (!entity) throw new Error("not a blueprint with entities");
	entity.tags = { payload: randomBytes(1_950_000).toString("base64") };
	return encode(node);
}
