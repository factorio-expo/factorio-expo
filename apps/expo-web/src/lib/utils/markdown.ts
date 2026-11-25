import { marked } from "marked";
import sanitizeHtml from "sanitize-html";
import { formatEscapedRichText } from "@factorio-expo/ui/game/rich-text.ts";

const INLINE_TAGS = ["p", "br", "strong", "em", "del", "code", "a"];
const BLOCK_TAGS = ["blockquote", "ul", "ol", "li", "pre", "hr"];
const HEADING_TAGS = ["h1", "h2", "h3", "h4", "h5", "h6"];
const TABLE_TAGS = ["table", "thead", "tbody", "tr", "th", "td"];

const baseOptions: sanitizeHtml.IOptions = {
	allowedAttributes: { a: ["href", "title"], ol: ["start"] },
	allowedSchemes: ["http", "https", "mailto"],
	allowProtocolRelative: false,
};

// Factorio rich text (icons, colors, bold) in the already escaped text nodes.
const richText = formatEscapedRichText;

/**
 * Entry descriptions are untrusted Markdown, never trusted HTML. A single
 * line break is kept (`breaks`): descriptions copied from the game are plain
 * lines, not Markdown paragraphs.
 */
export function renderMarkdown(source: string): string {
	return sanitizeHtml(marked.parse(source, { async: false, breaks: true }), {
		...baseOptions,
		allowedTags: [...INLINE_TAGS, ...BLOCK_TAGS, ...HEADING_TAGS, ...TABLE_TAGS],
		// textFilter receives escaped text, not attributes. Only our trusted
		// game-icon markup is inserted; user-authored spans/styles are disallowed.
		textFilter: (text, tagName) =>
			tagName === "code" || tagName === "pre" ? text : richText(text),
	});
}

// `#15`, not inside a word and not the `&#39;` of an escaped character.
const COMMENT_REF = /(^|[^&\w])#(\d{1,9})\b/g;

/**
 * Comments are untrusted Markdown with a tighter allow-list than
 * descriptions: no headings (they render as paragraphs) and no tables;
 * links are marked user-generated. `#N` becomes a link to comment N when
 * `refHref` gives one (it returns null for numbers the thread does not have),
 * except in code and inside links.
 */
export function renderCommentMarkdown(
	source: string,
	refHref: (number: number) => string | null,
): string {
	return sanitizeHtml(marked.parse(source, { async: false }), {
		...baseOptions,
		allowedTags: [...INLINE_TAGS, ...BLOCK_TAGS],
		allowedAttributes: { a: ["href", "title", "rel"], ol: ["start"] },
		transformTags: {
			...Object.fromEntries(HEADING_TAGS.map((tag) => [tag, "p"])),
			a: sanitizeHtml.simpleTransform("a", { rel: "nofollow ugc noopener" }),
		},
		textFilter: (text, tagName) => {
			if (tagName === "code" || tagName === "pre") return text;
			if (tagName === "a") return richText(text);
			// References first: their markup holds no icon tokens, while icon
			// markup must not be scanned for references.
			const linked = text.replace(COMMENT_REF, (match, before: string, digits: string) => {
				const href = refHref(Number(digits));
				return href === null
					? match
					: `${before}<a href="${escapeAttribute(href)}" class="comment-ref">#${digits}</a>`;
			});
			return richText(linked);
		},
	});
}

const escapeAttribute = (value: string) => value.replace(/&/g, "&amp;").replace(/"/g, "&quot;");
