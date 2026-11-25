import { describe, expect, it } from "vitest";
import { renderCommentMarkdown, renderMarkdown } from "./markdown";

const known = new Set([1, 2, 15]);
const href = (n: number) => (known.has(n) ? `#c-${n}` : null);
const render = (source: string) => renderCommentMarkdown(source, href);

describe("renderCommentMarkdown", () => {
	it("links #N references the thread has", () => {
		expect(render("see #15 and #99")).toBe(
			'<p>see <a href="#c-15" class="comment-ref">#15</a> and #99</p>\n',
		);
	});

	it("leaves references in code, links and words alone", () => {
		expect(render("`#1`")).toBe("<p><code>#1</code></p>\n");
		expect(render("[#2](https://example.com)")).toContain(">#2</a>");
		expect(render("[#2](https://example.com)")).not.toContain("comment-ref");
		expect(render("abc#1")).not.toContain("comment-ref");
	});

	it("does not read escaped characters as references", () => {
		const html = render("it&#39;s &#1; #1 &lt;#2");
		expect(html.match(/comment-ref/g)).toHaveLength(2);
		expect(html).not.toMatch(/&<a/);
	});

	it("escapes reference targets", () => {
		expect(renderCommentMarkdown("#1", () => '?a=1&b="x"#c-1')).toContain(
			'href="?a=1&amp;b=&quot;x&quot;#c-1"',
		);
	});

	it("strips unsafe markup", () => {
		const html = render(
			'<script>alert(1)</script><img src=x onerror=alert(1)>[x](javascript:alert(1))<span style="x">s</span>',
		);
		expect(html).not.toMatch(/<script|<img|onerror|href="javascript:|<span/);
	});

	it("renders headings as paragraphs and drops tables", () => {
		expect(render("# Title")).toBe("<p>Title</p>\n");
		expect(render("| a |\n|---|\n| b |")).not.toContain("<table");
	});

	it("marks links as user generated", () => {
		expect(render("[x](https://example.com)")).toContain('rel="nofollow ugc noopener"');
	});

	it("renders icon tags", () => {
		expect(render("[item=iron-ore] #1")).toMatch(
			/factorio-icon.*aria-label="item=iron-ore".*comment-ref/,
		);
	});
});

describe("renderMarkdown", () => {
	it("keeps single line breaks and renders rich text", () => {
		expect(renderMarkdown("one\n[color=red]two[/color]")).toBe(
			'<p>one<br /><span class="rich-text-color" style="--rich-text-color:rgb(255 42 36)">two</span></p>\n',
		);
	});
});
