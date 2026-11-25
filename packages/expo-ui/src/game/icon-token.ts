// The rich-text icon grammar: "[item=iron-plate]" or "[item=iron-plate,quality=rare]".
// Pure; the Go twin is internal/icontoken, and both are tested against the one
// fixture test-data/icon-tokens.json.
//
// Canonical form: "normal" quality is omitted. Search filters are the one place
// that spells it out (`formatToken(token, { keepNormal: true })`) because the
// index stores production facets with an explicit quality. Fluids have no
// quality in the game: a fluid token parses as quality-less whatever it spells
// out, and is formatted without one.

export const QUALITIES = ["normal", "uncommon", "rare", "epic", "legendary"] as const;

/** Tag types that render as icons. Other tags ([color=...], [font=...]) are not icons. */
export const ICON_TYPES: ReadonlySet<string> = new Set([
	"item",
	"entity",
	"recipe",
	"fluid",
	"virtual-signal",
	"planet",
	"space-location",
	"quality",
	"tile",
	"technology",
	"achievement",
	"item-group",
]);

export interface IconToken {
	type: string;
	name: string;
	/** "normal" when the token carries no quality, and always for fluids. */
	quality: string;
}

export interface ScannedToken extends IconToken {
	/** The token exactly as written, e.g. "[item=iron-plate,quality=rare]". */
	raw: string;
}

const SOURCE = String.raw`\[([a-zA-Z0-9-]+)=([a-zA-Z0-9_-]+)(?:,quality=([a-zA-Z0-9_-]+))?]`;

function toToken(type: string, name: string, quality: string | undefined): IconToken | undefined {
	if (!ICON_TYPES.has(type)) return undefined;
	return { type, name, quality: (type !== "fluid" && quality) || "normal" };
}

/** Parses one whole token; undefined for anything else, including non-icon tags. */
export function parseToken(text: string): IconToken | undefined {
	const match = new RegExp(`^${SOURCE}$`).exec(text);
	return match ? toToken(match[1], match[2], match[3]) : undefined;
}

export function formatToken(token: IconToken, options: { keepNormal?: boolean } = {}): string {
	const { type, name, quality } = token;
	const omit = type === "fluid" || !quality || (quality === "normal" && !options.keepNormal);
	return omit ? `[${type}=${name}]` : `[${type}=${name},quality=${quality}]`;
}

/**
 * Whether a token spells out its quality (so an explicit "normal" differs from
 * "any"). A fluid token never has one.
 */
export const hasQuality = (token: string) =>
	token.includes(",quality=") && parseToken(token)?.type !== "fluid";

/** Every icon token in `text`, in order. */
export function scanTokens(text: string): ScannedToken[] {
	const found: ScannedToken[] = [];
	for (const match of text.matchAll(new RegExp(SOURCE, "g"))) {
		const token = toToken(match[1], match[2], match[3]);
		if (token) found.push({ ...token, raw: match[0] });
	}
	return found;
}

/** Replaces each icon token with `replace(token)`; other text is untouched. */
export function replaceTokens(text: string, replace: (token: ScannedToken) => string): string {
	return text.replace(new RegExp(SOURCE, "g"), (raw, type, name, quality) => {
		const token = toToken(type, name, quality);
		return token ? replace({ ...token, raw }) : raw;
	});
}

/** `text` without its icon tokens. */
export function stripTokens(text: string): string {
	return replaceTokens(text, () => "");
}
