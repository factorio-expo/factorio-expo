/**
 * Best-effort Markdown to plain text for card previews, where block elements
 * (an h1, a list) would look wrong. Keeps text and icon tokens like
 * `[item=iron-plate]`; the result still goes through formatRichText.
 */
export function markdownToPlainText(source: string): string {
	return source
		.replace(/```[^\n]*\n?([\s\S]*?)```/g, "$1")
		.replace(/!\[([^\]]*)]\([^)]*\)/g, "$1")
		.replace(/\[([^\]]+)]\([^)]*\)/g, "$1")
		.replace(/^\s{0,3}(?:#{1,6}\s+|>\s?|[-*+]\s+|\d+[.)]\s+|[-*_]{3,}\s*$)/gm, "")
		.replace(/(\*\*|__|~~)(.+?)\1/g, "$2")
		.replace(/\*(\S(?:.*?\S)?)\*/g, "$1")
		.replace(/`([^`]*)`/g, "$1")
		.replace(/\s*\n\s*/g, " ")
		.trim();
}
