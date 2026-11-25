import { describe, expect, it, vi } from "vitest";
import { formatRichText, formatRichTextSummary, stripRichText } from "./rich-text";

// The generated icon table is game data; a fixed stand-in keeps these pure.
vi.mock("./game-icons", () => ({
	gameIcon: (type: string, name: string) =>
		(type === "item" && (name === "iron-plate" || name === "item-unknown")) ||
		(type === "utility" && name === "space-age")
			? `--icon:${type}.${name}`
			: undefined,
}));

const COLOR = (css: string) => `<span class="rich-text-color" style="--rich-text-color:${css}">`;

describe("formatRichText", () => {
	it("renders icon tags and the Space Age logo", () => {
		expect(formatRichText("[item=iron-plate]")).toContain('aria-label="item=iron-plate"');
		expect(formatRichText("[space-age] only")).toBe(
			'<span class="icon factorio-icon" style="--icon:utility.space-age" role="img" aria-label="Space Age"></span> only',
		);
	});

	it("colors by name, 0-1 and 0-255 components, and hex", () => {
		expect(formatRichText("[color=red]a[/color]")).toBe(`${COLOR("rgb(255 42 36)")}a</span>`);
		expect(formatRichText("[color=1,0,0.5]a[/color]")).toBe(
			`${COLOR("rgb(255 0 128)")}a</span>`,
		);
		expect(formatRichText("[color=255, 128, 0]a[/color]")).toBe(
			`${COLOR("rgb(255 128 0)")}a</span>`,
		);
		expect(formatRichText("[color=#ffbb00]a[/color]")).toBe(
			`${COLOR("rgb(255 187 0)")}a</span>`,
		);
		expect(formatRichText("[color=#ff000080]a[/color]")).toBe(
			`${COLOR("rgb(255 0 0 / 0.502)")}a</span>`,
		);
	});

	it("bolds default-bold and nests tags", () => {
		expect(
			formatRichText("[font=default-bold][color=red]a [item=iron-plate][/color][/font]"),
		).toMatch(
			/^<strong><span class="rich-text-color"[^>]*>a <span class="icon[^>]*><\/span><\/span><\/strong>$/,
		);
	});

	it("leaves unsupported, unpaired and crossed tags as written", () => {
		for (const text of [
			"[gps=10,20,nauvis]",
			"[img=item/iron-plate]",
			"[train=12] [train-stop=3] [tooltip=a,b]",
			"[font=heading-1]big[/font]",
			"[color=notacolor]x[/color]",
			"[color=red]never closed",
			"stray [/color]",
			"[technology=steel-processing]",
		]) {
			expect(formatRichText(text)).toBe(text);
		}
		expect(formatRichText("[color=red][font=default-bold]x[/color][/font]")).toBe(
			"[color=red]<strong>x[/color]</strong>",
		);
		// An unsupported font still pairs with its own close, not an outer one.
		expect(formatRichText("[color=red][font=x]a[/font]b[/color]")).toBe(
			`${COLOR("rgb(255 42 36)")}[font=x]a[/font]b</span>`,
		);
	});

	it("never lets values reach the markup unescaped", () => {
		const html = formatRichText('[color="><script>]x[/color]<b>');
		expect(html).not.toMatch(/<script|<b>/);
		expect(html).toContain("&lt;b&gt;");
	});

	it("keeps line breaks", () => {
		expect(formatRichText("one\ntwo\r\nthree")).toBe("one<br>two<br>three");
	});
});

describe("formatRichTextSummary", () => {
	it("drops tags formatRichText leaves as written but keeps their text", () => {
		expect(
			formatRichTextSummary(
				"Line [font=default-large]big[/font] at [gps=1,2] [color=red]open [WIP]",
			),
		).toBe("Line big at  open [WIP]");
	});

	it("still renders supported tags", () => {
		expect(formatRichTextSummary("[color=red]a[/color] [font=default-bold]b[/font]")).toBe(
			`${COLOR("rgb(255 42 36)")}a</span> <strong>b</strong>`,
		);
	});
});

describe("stripRichText", () => {
	it("drops icons, the logo and color/font tags but keeps other tags", () => {
		expect(
			stripRichText(
				"[color=red]Red[/color] [font=default-bold]bold[/font][space-age][item=iron-plate] [gps=1,2]",
			),
		).toBe("Red bold [gps=1,2]");
	});

	it("keeps the text of unsupported or unpaired color/font tags", () => {
		expect(
			stripRichText("[font=heading-1]Big[/font] [color=nope]x[/color] [color=red]open"),
		).toBe("Big x open");
	});
});
