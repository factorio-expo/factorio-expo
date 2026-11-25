import type { MaterialRate, Production, SnapToGrid } from "../api";
import { formatToken } from "@factorio-expo/ui/game/icon-token.ts";

/**
 * Rate units of the production card's segmented control: time units show
 * their `suffix`, belt units the belt item's icon (`beltItem`).
 */
export const productionTimescales: {
	value: string;
	label: string;
	multiplier: number;
	suffix: string;
	belt?: true;
	beltItem?: string;
}[] = [
	{ value: "second", label: "Per second", multiplier: 1, suffix: "/s" },
	{ value: "minute", label: "Per minute", multiplier: 60, suffix: "/min" },
	{ value: "hour", label: "Per hour", multiplier: 3600, suffix: "/h" },
	{
		value: "belt",
		label: "Per transport belt (15 items/s)",
		multiplier: 1 / 15,
		suffix: " belts",
		belt: true,
		beltItem: "transport-belt",
	},
	{
		value: "fast-belt",
		label: "Per fast transport belt (30 items/s)",
		multiplier: 1 / 30,
		suffix: " belts",
		belt: true,
		beltItem: "fast-transport-belt",
	},
	{
		value: "express-belt",
		label: "Per express transport belt (45 items/s)",
		multiplier: 1 / 45,
		suffix: " belts",
		belt: true,
		beltItem: "express-transport-belt",
	},
	{
		value: "turbo-belt",
		label: "Per turbo transport belt (60 items/s)",
		multiplier: 1 / 60,
		suffix: " belts",
		belt: true,
		beltItem: "turbo-transport-belt",
	},
];

/**
 * A rate in the chosen unit. `ratio` rates (per tile, per MW) are small, so
 * they keep three significant digits instead of two decimals.
 */
export function formatRate(
	value: number | undefined,
	timescale = productionTimescales[0],
	type: string = "item",
	factor = 1,
	ratio = false,
): string {
	const scale = timescale.belt && type !== "item" ? productionTimescales[0] : timescale;
	const scaled = (value ?? 0) * scale.multiplier * factor;
	const text = ratio
		? scaled.toLocaleString("en-US", { maximumSignificantDigits: 3 })
		: (Math.round(scaled * 100) / 100).toLocaleString("en-US", { maximumFractionDigits: 2 });
	return `${text}${scale.suffix}`;
}

/**
 * What a leaf's output rates are divided by for "per tile" and "per MW": its
 * footprint area in tiles and the electricity it consumes in MW (drain
 * included). Either is absent when unknown or not positive, so no ratio is
 * shown (the same rule as the search index's rate_tile_out_ and rate_mw_out_
 * fields).
 */
export function rateDivisors(production: Production): { tiles?: number; megawatts?: number } {
	const footprint = production.footprint;
	const tiles = footprint ? footprint.width * footprint.height : 0;
	const megawatts = (production.electricity?.consumption_watts ?? 0) / 1e6;
	return {
		tiles: tiles > 0 ? tiles : undefined,
		megawatts: megawatts > 0 ? megawatts : undefined,
	};
}

/** A power in kW, whole numbers ("1,800 kW"). */
export function formatPower(watts: number): string {
	return `${(watts / 1000).toLocaleString("en-US", { maximumFractionDigits: 0 })} kW`;
}

/**
 * An exchange string's size. Exchange strings are base64 ASCII, so its
 * characters are its bytes: "1,234 characters (1.2 kB)".
 */
export function formatStringSize(exchange: string): string {
	const length = exchange.length;
	const bytes =
		length < 1000
			? `${length} B`
			: `${(length / 1000).toLocaleString("en-US", { maximumFractionDigits: 1 })} kB`;
	return `${length.toLocaleString("en-US")} characters (${bytes})`;
}

// Builds the formatRichText icon token (e.g. "[item=iron-plate]") for a
// production material rate row.
export function materialIconToken(material: MaterialRate): string {
	return formatToken({
		type: material.type,
		name: material.name,
		quality: material.quality ?? "",
	});
}

/** A build-cost amount for an icon badge: whole numbers below 1000 (at most
 * one decimal for fractions), compact above ("12.3k"). */
export function formatAmount(value: number): string {
	if (value >= 1000) {
		return value.toLocaleString("en-US", { notation: "compact", maximumFractionDigits: 1 });
	}
	return value.toLocaleString("en-US", { maximumFractionDigits: value < 10 ? 1 : 0 });
}

/** "32 × 16" for a footprint or grid cell. */
export function formatSize(width: number, height: number): string {
	return `${width} × ${height}`;
}

/** A snap-to-grid setting in words: "32 × 16 tiles, absolute (offset 3, 7)", or "Off". */
export function formatSnapToGrid(snap: SnapToGrid | undefined): string {
	if (!snap) return "Off";
	const position = snap.absolute
		? `absolute (offset ${snap.offset_x}, ${snap.offset_y})`
		: "relative";
	return `${formatSize(snap.width, snap.height)} tiles, ${position}`;
}
