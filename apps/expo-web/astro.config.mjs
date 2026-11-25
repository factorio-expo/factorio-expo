// @ts-check
import { defineConfig, envField, fontProviders } from "astro/config";
import { browserslistToTargets } from "lightningcss";
import browserslist from "browserslist";

import node from "@astrojs/node";
import sitemap from "@astrojs/sitemap";
import svelte from "@astrojs/svelte";
import gameIcons from "@factorio-expo/ui/game/integration.ts";

import { markdownEditorStyleHashes } from "@factorio-expo/ui/forms/markdown-editor/csp-hashes.mjs";
import { purgeCss } from "./purge-css.mjs";

// Origins the browser loads API data and preview images from, and the icon
// sheets from. PUBLIC_* values are inlined at build time, so the CSP is too.
// A relative PUBLIC_API_BASE_URL (e.g. "/") means the API is on the site's own
// origin: the reverse proxy in production, the dev proxy below under `astro dev`.
const apiBase = process.env.PUBLIC_API_BASE_URL || "http://localhost:8080";
const apiOrigin = /^https?:\/\//.test(apiBase) ? new URL(apiBase).origin : "'self'";
// Where the dev server forwards /api (the /api/v1 API; only `astro dev` uses it).
const devApiTarget = process.env.API_INTERNAL_BASE_URL || "http://localhost:8080";
const iconsBase = process.env.PUBLIC_GAME_ICONS_BASE_URL || "/game-icons/";
// The public origin (canonical links, sitemap, feeds): set per deployment.
const siteUrl = process.env.PUBLIC_SITE_URL || "http://localhost:3000";
const iconOrigin = /^https?:\/\//.test(iconsBase) ? new URL(iconsBase).origin : "'self'";

// https://astro.build/config
export default defineConfig({
	site: siteUrl,
	// One URL per page: /about/ redirects to /about (canonical links agree).
	trailingSlash: "never",
	// The conventional sitemap URL; @astrojs/sitemap writes an index instead.
	redirects: {
		"/sitemap.xml": "/sitemap-index.xml",
	},
	compressHTML: true,

	build: {
		assets: "assets",
		inlineStylesheets: "never",
	},

	// Every page depends on the viewer (the layout's header and sidebar show
	// who is signed in) or on route params and query strings, so pages render
	// on demand. Only the generated text files (robots.txt, security.txt,
	// opensearch.xml) opt in with `export const prerender = true`.
	output: "server",

	// Astro hashes every script and stylesheet it emits into a per-page CSP
	// <meta> tag, so injected markup cannot run script. Inline style
	// *attributes* (game-icon sprite positions) are the one inline allowance;
	// the markdown editor's runtime <style> is allowed by hash.
	// frame-ancestors etc. are sent as a header by src/middleware.ts. The two
	// pages with a sign-up form add Cloudflare Turnstile's script and frame
	// at render time (Astro.csp, lib/turnstile.ts allowTurnstile).
	security: {
		csp: {
			directives: [
				"default-src 'self'",
				`img-src 'self' data: ${apiOrigin} ${iconOrigin}`,
				`connect-src 'self' ${apiOrigin}`,
				"font-src 'self'",
				"object-src 'none'",
				"base-uri 'self'",
				"form-action 'self'",
			],
			scriptDirective: { resources: ["'self'"] },
			styleDirective: {
				resources: ["'self'", { resource: "'unsafe-inline'", kind: "attribute" }],
				hashes: markdownEditorStyleHashes(),
			},
		},
	},
	adapter: node({
		// Standalone mode: the container's only process is this Node server
		// (no separate reverse proxy in front of it), matching how the other
		// apps/ services run as single-process containers.
		mode: "standalone",
	}),
	server: {
		host: true,
		port: 4321,
	},
	env: {
		schema: {
			// Read by both server-side (SSR) and browser code: the URL a
			// browser uses to reach the API. In docker-compose this is the
			// published host port; outside compose it's wherever expo-api runs.
			PUBLIC_API_BASE_URL: envField.string({
				context: "client",
				access: "public",
				default: "http://localhost:8080",
			}),
			PUBLIC_GAME_ICONS_BASE_URL: envField.string({
				context: "client",
				access: "public",
				default: "/game-icons/",
			}),
			// Cloudflare Turnstile site key for the sign-up forms; unset turns
			// the human check off. Set it together with the API's
			// API_TURNSTILE_SECRET_KEY.
			PUBLIC_TURNSTILE_SITE_KEY: envField.string({
				context: "client",
				access: "public",
				optional: true,
			}),
			// Who runs the site, as JSON: {"name", "email", "hostingProvider",
			// "emailProvider"} (lib/legal.ts). Kept out of the repository; a
			// missing field shows as an [OWNER: ...] placeholder.
			PUBLIC_LEGAL: envField.string({
				context: "client",
				access: "public",
				optional: true,
			}),
			// Read only during SSR: the URL this container uses to reach
			// expo-api over the compose network (service DNS name, not a
			// published host port). Falls back to the public URL so `astro dev`
			// works without extra config outside compose.
			API_INTERNAL_BASE_URL: envField.string({
				context: "server",
				access: "secret",
				optional: true,
			}),
		},
	},
	vite: {
		plugins: [
			purgeCss({
				// No built HTML exists (everything renders on demand), so the source is the content.
				content: [
					"./src/**/*.{astro,js,ts,svelte}",
					"../../packages/expo-ui/src/**/*.{astro,js,ts,svelte}",
				],
				fontFace: false,
				keyframes: true,
				variables: false,
				// Scope classes are added at compile time, so never appear in the source;
				// the :hover/:active/:focus entries keep those branches of Pico's :is() selectors.
				safelist: [
					/^factorio-icon/,
					/^data-astro-cid-/,
					/^svelte-/,
					":hover",
					":active",
					":focus",
				],
			}),
		],
		build: {
			cssMinify: "lightningcss",
		},
		css: {
			preprocessorOptions: { scss: { quietDeps: true } },
			transformer: "lightningcss",
			lightningcss: {
				targets: browserslistToTargets(browserslist(">= 0.25%")),
			},
		},
		resolve: {
			alias: {
				"@styles": "/src/styles",
			},
		},
		// `astro dev` only: serve the API on the site's origin, so with
		// PUBLIC_API_BASE_URL=/ the site works from localhost and the LAN alike
		// (docs/frontend.md#local-development). X-Real-IP stands in for the reverse proxy.
		server: {
			proxy: {
				"/api": {
					target: devApiTarget,
					configure: (proxy) => {
						proxy.on("proxyReq", (proxyReq, req) => {
							if (req.socket.remoteAddress) {
								proxyReq.setHeader("X-Real-IP", req.socket.remoteAddress);
							}
						});
					},
				},
			},
		},
	},
	fonts: [
		{
			provider: fontProviders.google(),
			name: "Doppio One",
			cssVariable: "--font-doppio-one",
			styles: ["normal"],
			subsets: ["latin"],
			fallbacks: ["sans-serif"],
		},
		{
			provider: fontProviders.google(),
			name: "Ubuntu Sans Mono",
			cssVariable: "--font-ubuntu-sans-mono",
			weights: [400, 700],
			styles: ["normal"],
			subsets: ["latin"],
			fallbacks: ["monospace"],
		},
	],
	// No markdown pages: skip Shiki (it warns at startup otherwise).
	markdown: {
		syntaxHighlight: false,
	},
	// Hover prefetch makes entry and search navigation feel instant.
	prefetch: { prefetchAll: false, defaultStrategy: "hover" },
	devToolbar: { enabled: false },
	integrations: [
		svelte(),
		gameIcons(),
		sitemap({
			// Session-only and sign-in pages are not search landing pages; compare
			// needs two blueprints in its query and is noindex.
			filter: (page) =>
				!/\/(account|account-deleted|login|signup|recover|recovery-code|compare)(\/|$)/.test(
					new URL(page).pathname,
				),
			// Entries render on demand; src/pages/sitemap-entries.xml.ts lists them.
			customSitemaps: [`${siteUrl}/sitemap-entries.xml`],
		}),
	],
});
