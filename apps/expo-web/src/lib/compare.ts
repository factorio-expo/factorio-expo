// The metric table of the compare page: one row per metric with an A and a B
// cell, built from what the two blueprints' analyses expose. Pure, so it is
// unit tested; the page only renders it.
import type { CompareLeaf, MaterialRate } from "@lib/api";
import { formatToken } from "@factorio-expo/ui/game/icon-token.ts";
import { formatAmount, formatPower, formatRate, formatSize } from "@utils/production.ts";

/** What a missing value reads as. */
export const MISSING = "—";

/** The most item or fluid rows of one group; the largest by rate or amount come first. */
export const MAX_ROWS = 12;

export interface CompareRow {
	/** Stable name for tests and selectors. */
	id: string;
	label: string;
	/** An icon beside the label (formatRichText token) for an item or fluid row. */
	iconToken?: string;
	a: string;
	b: string;
	/** Which side has the larger figure; unset for text, a tie or a side without a figure. */
	larger?: "a" | "b";
}

/** A formatted figure with its number, when it has one to compare. */
interface Measure {
	text: string;
	value?: number;
}

const measure = (text: string, value?: number): Measure => ({ text, value });

export interface CompareGroup {
	id: string;
	title: string;
	rows: CompareRow[];
	/** How many item or fluid rows were left out past MAX_ROWS. */
	omitted: number;
}

type Production = CompareLeaf["production"];

/** Whether the leaf has a finished analysis, so its figures mean something. */
export const analysed = (leaf: CompareLeaf) => leaf.production.availability === "ready";

/** What the leaf's analysis says, or undefined while it has none. */
const fromAnalysis = <T>(leaf: CompareLeaf, read: (production: Production) => T | undefined) =>
	analysed(leaf) ? read(leaf.production) : undefined;

function row(
	id: string,
	label: string,
	a: Measure | undefined,
	b: Measure | undefined,
	iconToken?: string,
): CompareRow {
	const [left, right] = [a?.value, b?.value];
	const larger =
		left === undefined || right === undefined || left === right
			? undefined
			: left > right
				? "a"
				: "b";
	return { id, label, iconToken, a: a?.text ?? MISSING, b: b?.text ?? MISSING, larger };
}

/** A fixed-metric group: a row per metric, formatted per side. */
function metricGroup(
	id: string,
	title: string,
	a: CompareLeaf,
	b: CompareLeaf,
	metrics: { id: string; label: string; read: (leaf: CompareLeaf) => Measure | undefined }[],
): CompareGroup {
	return {
		id,
		title,
		omitted: 0,
		rows: metrics.map((metric) => row(metric.id, metric.label, metric.read(a), metric.read(b))),
	};
}

const materialKey = (material: MaterialRate) =>
	`${material.type}:${material.name}:${material.quality ?? ""}`;

/**
 * A group of item or fluid rows: the union of both sides' materials with each
 * side's `value`, the largest of either side first, at most MAX_ROWS.
 */
function materialGroup(
	id: string,
	title: string,
	a: MaterialRate[],
	b: MaterialRate[],
	value: (material: MaterialRate) => number | undefined,
	format: (amount: number) => string,
): CompareGroup {
	const figure = (amount: number | undefined) =>
		amount === undefined ? undefined : measure(format(amount), amount);
	const bySide = [a, b].map((side) => new Map(side.map((m) => [materialKey(m), m] as const)));
	const keys = new Set(bySide.flatMap((side) => [...side.keys()]));
	const ranked = [...keys]
		.map((key) => {
			const [left, right] = bySide.map((side) => side.get(key));
			return {
				key,
				material: (left ?? right)!,
				a: left && value(left),
				b: right && value(right),
			};
		})
		.filter((entry) => entry.a !== undefined || entry.b !== undefined)
		.toSorted(
			(x, y) =>
				Math.max(y.a || 0, y.b || 0) - Math.max(x.a || 0, x.b || 0) ||
				x.key.localeCompare(y.key),
		);
	const rows = ranked.slice(0, MAX_ROWS).map(({ key, material, a: left, b: right }) =>
		row(
			`${id}-${key}`,
			material.name,
			figure(left),
			figure(right),
			formatToken({
				type: material.type,
				name: material.name,
				quality: material.quality ?? "",
			}),
		),
	);
	return { id, title, rows, omitted: Math.max(0, ranked.length - MAX_ROWS) };
}

/** The raw build cost as materials, so it ranks and renders like a rate group. */
const rawCostMaterials = (leaf: CompareLeaf): MaterialRate[] =>
	(fromAnalysis(leaf, (p) => p.build_cost?.raw) ?? []).map((raw) => ({
		type: raw.type,
		name: raw.name,
		quality: null,
		output_per_second: raw.amount,
	}));

const electricity =
	(read: (power: NonNullable<Production["electricity"]>) => number) => (leaf: CompareLeaf) => {
		const power = fromAnalysis(leaf, (p) => p.electricity);
		return power ? measure(formatPower(read(power)), read(power)) : undefined;
	};

const platform =
	(
		read: (stats: NonNullable<Production["space_platform"]>) => number | undefined,
		unit: string,
	) =>
	(leaf: CompareLeaf) => {
		const stats = fromAnalysis(leaf, (p) => p.space_platform);
		const value = stats?.is_space_platform ? read(stats) : undefined;
		return value === undefined
			? undefined
			: measure(
					`${value.toLocaleString("en-US", { maximumFractionDigits: 2 })} ${unit}`,
					value,
				);
	};

const footprint = (leaf: CompareLeaf) => fromAnalysis(leaf, (p) => p.footprint);

const count = (leaf: CompareLeaf) =>
	measure(leaf.entity_count.toLocaleString("en-US"), leaf.entity_count);

/**
 * The compare table: overview, power and pollution, space platform (only when
 * either side is one), the products and ingredients per second, and the build
 * cost. A blueprint without a finished analysis shows "—" for everything the
 * analysis gives; rows and groups with nothing to show are left out.
 */
export function compareGroups(a: CompareLeaf, b: CompareLeaf): CompareGroup[] {
	const groups: CompareGroup[] = [
		metricGroup("overview", "Overview", a, b, [
			{ id: "entities", label: "Entities", read: count },
			{
				id: "footprint",
				label: "Footprint",
				read: (leaf) => {
					const size = footprint(leaf);
					return size
						? measure(`${formatSize(size.width, size.height)} tiles`)
						: undefined;
				},
			},
			{
				id: "area",
				label: "Area",
				read: (leaf) => {
					const size = footprint(leaf);
					return size
						? measure(
								`${(size.width * size.height).toLocaleString("en-US")} tiles`,
								size.width * size.height,
							)
						: undefined;
				},
			},
			{
				id: "game-version",
				label: "Game version",
				read: (leaf) => (leaf.game_version ? measure(leaf.game_version) : undefined),
			},
		]),
		metricGroup("power", "Power and pollution", a, b, [
			{
				id: "consumption",
				label: "Power used",
				read: electricity((e) => e.consumption_watts),
			},
			{ id: "production", label: "Power made", read: electricity((e) => e.production_watts) },
			{ id: "net-power", label: "Net power", read: electricity((e) => e.net_watts) },
			{
				id: "pollution",
				label: "Pollution (net)",
				read: (leaf) => {
					const pollution = fromAnalysis(leaf, (p) => p.pollution);
					return pollution && !pollution.not_applicable
						? measure(formatRate(pollution.net_per_second), pollution.net_per_second)
						: undefined;
				},
			},
		]),
		metricGroup("platform", "Space platform", a, b, [
			{ id: "weight", label: "Weight", read: platform((p) => p.weight_tons, "t") },
			{ id: "thrust", label: "Thrust", read: platform((p) => p.thrust_mn, "MN") },
			{ id: "speed", label: "Top speed", read: platform((p) => p.max_speed_km_s, "km/s") },
		]),
		materialGroup(
			"products",
			"Products per second",
			fromAnalysis(a, (p) => p.products) ?? [],
			fromAnalysis(b, (p) => p.products) ?? [],
			(material) => material.output_per_second,
			(amount) => formatRate(amount),
		),
		materialGroup(
			"ingredients",
			"Ingredients per second",
			fromAnalysis(a, (p) => p.ingredients) ?? [],
			fromAnalysis(b, (p) => p.ingredients) ?? [],
			(material) => material.input_per_second,
			(amount) => formatRate(amount),
		),
		metricGroup("cost", "Build cost", a, b, [
			{
				id: "build-items",
				label: "Items to build",
				read: (leaf) => {
					const items = fromAnalysis(leaf, (p) => p.build_cost?.items);
					if (!items) return undefined;
					const total = items.reduce((sum, item) => sum + item.count, 0);
					return measure(total.toLocaleString("en-US"), total);
				},
			},
		]),
		materialGroup(
			"raw-cost",
			"Raw resources to build",
			rawCostMaterials(a),
			rawCostMaterials(b),
			(material) => material.output_per_second,
			(amount) => formatAmount(amount),
		),
	];
	return groups
		.map((group) =>
			Object.assign({}, group, {
				rows: group.rows.filter((entry) => entry.a !== MISSING || entry.b !== MISSING),
			}),
		)
		.filter((group) => group.rows.length > 0);
}
