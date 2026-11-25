// Build-time CSP hashes for <style> elements that third-party code injects at
// runtime (Astro only hashes what it renders itself).
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";

const require = createRequire(import.meta.url);

/**
 * markdown-text-editor injects its compiled CSS as one <style> element
 * (`document.createTextNode(`...`)` in its ES build). Hash exactly that text so
 * the CSP can allow it without 'unsafe-inline'. Returns [] (styles blocked,
 * editor unstyled) if the bundle shape changes, with a build warning.
 * @returns {`sha256-${string}`[]}
 */
export function markdownEditorStyleHashes() {
	try {
		const entry = require.resolve("markdown-text-editor");
		const source = readFileSync(join(dirname(entry), "markdown-text-editor.es.js"), "utf8");
		const match = source.match(/createTextNode\(`((?:[^`\\]|\\[\s\S])*)`\)/);
		if (!match) throw new Error("injected stylesheet not found");
		// Evaluate the template literal's escapes exactly as the browser will.
		// oxlint-disable-next-line typescript/no-implied-eval -- build-time, input is our own dependency
		const css = new Function(`return \`${match[1]}\`;`)();
		return [`sha256-${createHash("sha256").update(css).digest("base64")}`];
	} catch (err) {
		console.warn(`[csp] markdown-text-editor style hash unavailable: ${String(err)}`);
		return [];
	}
}
