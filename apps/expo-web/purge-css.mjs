// Vite plugin: drops unused CSS (mostly Pico's) from every stylesheet module
// at build time, before Vite hashes the file names. A name then changes
// exactly when its purged content does, so unchanged CSS keeps its
// long-lived CDN cache entry across deploys.
//
// A Vite plugin rather than a PostCSS plugin because `css.transformer:
// "lightningcss"` skips PostCSS plugins entirely. At normal order it runs
// after Vite's own CSS compile (Sass, scoped Astro/Svelte styles) and before
// vite:css-post turns the stylesheet into a bundled asset.
import { PurgeCSS } from "purgecss";

/** @param {Omit<import("purgecss").UserDefinedOptions, "css">} options */
export function purgeCss(options) {
	return {
		name: "expo-purge-css",
		apply: "build",
		async transform(code, id) {
			if (!/\.(css|scss)(\?|$)|[?&]lang\.css(&|$)/.test(id)) return null;
			const [result] = await new PurgeCSS().purge({ ...options, css: [{ raw: code }] });
			return { code: result.css, map: null };
		},
	};
}
