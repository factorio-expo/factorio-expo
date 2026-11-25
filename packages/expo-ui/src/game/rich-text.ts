// Factorio rich text (https://wiki.factorio.com/Rich_text): rendered as safe
// HTML, or stripped to plain text.
import { gameIcon } from "./game-icons";
import { replaceTokens, stripTokens } from "./icon-token";

/** A [color=...]/[font=...] opening tag (kind, value) or a closing one (kind). */
export const STYLE_TAG = /\[(color|font)=([^\][]*)]|\[\/(color|font)]/g;

/**
 * Plain text for contexts (e.g. <title>, textContent) that escape it
 * themselves: icon tokens and [space-age] are dropped, and every color/font
 * tag (any value, paired or not) is removed while the text inside it stays.
 * Never pass the result to set:html.
 */
export function stripRichText(text: string): string {
	return stripTokens(text.replace(STYLE_TAG, "")).replaceAll("[space-age]", "");
}

const HTML_ESCAPES: Record<string, string> = {
	"&": "&amp;",
	"<": "&lt;",
	">": "&gt;",
	'"': "&quot;",
	"'": "&#39;",
};

function escapeHtml(text: string): string {
	return text.replace(/[&<>"']/g, (char) => HTML_ESCAPES[char]);
}

// Factorio rich text (https://wiki.factorio.com/Rich_text) as safe HTML. The
// text is HTML-escaped first; then only these tags become markup, built from
// validated values, so user input cannot break out of it or inject attributes:
// - icon tags ([item=iron-plate], [virtual-signal=signal-A,quality=rare], ...)
//   and [space-age], as spritesheet-backed spans;
// - [color=...]...[/color], a colored span;
// - [font=default-bold]...[/font], bold.
// Every other tag ([gps=...], [img=...], [train=...], other fonts, a color
// the game would not read, an unclosed or crossed pair) stays as written.
// Catalog-like tokens whose icon is missing from the sheets render as the
// generic unknown item icon instead of leaking the raw token.
const UNKNOWN_ICON_TYPES = new Set(["item", "entity", "recipe", "fluid", "tile"]);

/** The game's named rich-text colors (utility-constants player_colors chat_color). */
const NAMED_COLORS: Record<string, readonly [number, number, number]> = {
	default: [1, 0.63, 0.259],
	red: [1, 0.166, 0.141],
	green: [0.173, 0.824, 0.25],
	blue: [0.343, 0.683, 1],
	orange: [1, 0.63, 0.259],
	yellow: [1, 0.828, 0.231],
	pink: [1, 0.52, 0.633],
	purple: [0.821, 0.44, 0.998],
	white: [0.9, 0.9, 0.9],
	black: [0.5, 0.5, 0.5],
	gray: [0.7, 0.7, 0.7],
	brown: [0.757, 0.522, 0.371],
	cyan: [0.335, 0.918, 0.866],
	acid: [0.708, 0.996, 0.134],
};

const BOLD_FONTS = new Set(["default-bold"]);

const NUMBER = String.raw`\d+(?:\.\d+)?|\.\d+`;
const COMPONENTS = new RegExp(`^(${NUMBER}),(${NUMBER}),(${NUMBER})(?:,(${NUMBER}))?$`);

/** A CSS color for a [color=...] value, or undefined when the game would not read it. */
function cssColor(value: string): string | undefined {
	const spec = value.trim().toLowerCase();
	let rgba: number[];
	const named = NAMED_COLORS[spec];
	const hex = /^#?([0-9a-f]{6}|[0-9a-f]{8})$/.exec(spec);
	const components = COMPONENTS.exec(spec.replace(/\s+/g, ""));
	if (named) {
		rgba = named.map((part) => Math.round(part * 255));
	} else if (hex) {
		rgba = hex[1].match(/../g)!.map((part) => parseInt(part, 16));
	} else if (components) {
		const parts = components
			.slice(1)
			.filter((part) => part !== undefined)
			.map(Number);
		// Like the game: any component above 1 means the 0-255 scale.
		const scale = parts.some((part) => part > 1) ? 1 : 255;
		rgba = parts.map((part) => Math.round(Math.min(255, part * scale)));
	} else {
		return undefined;
	}
	const [r, g, b, a] = rgba;
	return a === undefined || a === 255
		? `rgb(${r} ${g} ${b})`
		: `rgb(${r} ${g} ${b} / ${Math.round((a / 255) * 1000) / 1000})`;
}

interface StyleTag {
	start: number;
	end: number;
	/** Opening markup; undefined for a closing tag. */
	open?: string;
	kind: string;
}

/**
 * Replaces both tags of each properly nested, supported [color]/[font] pair
 * in `text` with `render(tag)`. Unpaired tags and unsupported values stay as
 * written.
 */
function replaceStyleTags(text: string, render: (tag: StyleTag) => string): string {
	const tags: StyleTag[] = [];
	for (const match of text.matchAll(STYLE_TAG)) {
		const [raw, openKind, value, closeKind] = match;
		const start = match.index;
		const end = start + raw.length;
		if (closeKind) {
			tags.push({ start, end, kind: closeKind });
			continue;
		}
		let open: string | undefined;
		if (openKind === "color") {
			const color = cssColor(value);
			open = color && `<span class="rich-text-color" style="--rich-text-color:${color}">`;
		} else if (BOLD_FONTS.has(value)) {
			open = "<strong>";
		}
		// An unsupported value still opens a pair, so its [/...] is not
		// mistaken for the close of an outer supported tag.
		tags.push({ start, end, kind: openKind, open: open ?? "" });
	}
	const stack: StyleTag[] = [];
	const paired: StyleTag[] = [];
	for (const tag of tags) {
		if (tag.open !== undefined) {
			stack.push(tag);
		} else if (stack.at(-1)?.kind === tag.kind) {
			const open = stack.pop()!;
			if (open.open) paired.push(open, { ...tag, open: undefined });
		}
	}
	paired.sort((a, b) => a.start - b.start);
	let html = "";
	let cursor = 0;
	for (const tag of paired) {
		html += text.slice(cursor, tag.start) + render(tag);
		cursor = tag.end;
	}
	return html + text.slice(cursor);
}

const closingMarkup = (kind: string) => (kind === "color" ? "</span>" : "</strong>");

const iconSpan = (style: string, label: string) =>
	`<span class="icon factorio-icon" style="${style}" role="img" aria-label="${label}"></span>`;

/**
 * Rich text over already HTML-escaped text: icon, color and font tags become
 * markup. For the Markdown renderer, which escapes text nodes itself.
 */
export function formatEscapedRichText(escaped: string): string {
	const styled = replaceStyleTags(escaped, (tag) => tag.open ?? closingMarkup(tag.kind));
	const iconed = replaceTokens(styled, ({ raw, type, name, quality }) => {
		const style =
			gameIcon(type, name, quality) ??
			(UNKNOWN_ICON_TYPES.has(type) ? gameIcon("item", "item-unknown") : undefined);
		return style
			? iconSpan(
					style,
					`${type}=${name}${quality !== "normal" ? `, quality=${quality}` : ""}`,
				)
			: raw;
	});
	const spaceAge = gameIcon("utility", "space-age");
	return spaceAge ? iconed.replaceAll("[space-age]", iconSpan(spaceAge, "Space Age")) : iconed;
}

/** User-supplied titles/descriptions as safe HTML; line breaks are kept. */
export function formatRichText(text: string): string {
	return formatEscapedRichText(escapeHtml(text)).replace(/\r?\n/g, "<br>");
}

/** A tag left over after formatting: `[name=value]` or `[/name]`. */
const LEFTOVER_TAG = /\[(?:[a-z][a-z0-9-]*=[^\][]*|\/[a-z][a-z0-9-]*)]/gi;

/**
 * formatRichText for compact summaries (entry cards): tags it would leave as
 * written ([gps=...], [font=default-large], an unpaired [color]) are dropped
 * and the text between them kept, so a card never shows raw markup.
 */
export function formatRichTextSummary(text: string): string {
	return formatRichText(text).replace(LEFTOVER_TAG, "");
}
