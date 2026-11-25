// @ts-check
import { defineConfig, envField, fontProviders } from "astro/config";

import node from "@astrojs/node";
import svelte from "@astrojs/svelte";
import gameIcons from "@factorio-expo/ui/game/integration.ts";
import { markdownEditorStyleHashes } from "@factorio-expo/ui/forms/markdown-editor/csp-hashes.mjs";

// The staff console (docs/admin.md). Its own app and container, so no admin
// code ships in the public bundle. Every page renders per request for a
// signed-in staff member; nothing is prerendered.
export default defineConfig({
	trailingSlash: "never",
	compressHTML: true,
	output: "server",
	build: {
		assets: "assets",
		inlineStylesheets: "never",
	},
	security: {
		// The console checks Origin itself (src/middleware.ts) against
		// ADMIN_ORIGIN, read at runtime: behind the proxy, Astro sees plain HTTP
		// and would refuse every form post.
		checkOrigin: false,
		// Hashed per-page script and style CSP. form-action and
		// frame-ancestors are sent as a header by src/middleware.ts.
		csp: {
			directives: [
				"default-src 'self'",
				"img-src 'self' data:",
				"connect-src 'self'",
				"font-src 'self'",
				"object-src 'none'",
				"base-uri 'self'",
			],
			scriptDirective: { resources: ["'self'"] },
			// Inline style attributes are the game icons' sprite positions; the
			// Markdown editor's runtime <style> is allowed by hash.
			styleDirective: {
				resources: ["'self'", { resource: "'unsafe-inline'", kind: "attribute" }],
				hashes: markdownEditorStyleHashes(),
			},
		},
	},
	adapter: node({ mode: "standalone" }),
	// No markdown pages: skip Shiki (it warns about the CSP otherwise).
	markdown: { syntaxHighlight: false },
	// Svelte islands from @factorio-expo/ui (icon picker, filter rows) and the
	// challenge rules editor; gameIcons serves the sprite sheets.
	integrations: [svelte(), gameIcons()],
	server: {
		host: true,
		port: 4321,
	},
	env: {
		schema: {
			// The console's public origin, e.g. https://console.example.org.
			ADMIN_ORIGIN: envField.string({ context: "server", access: "secret" }),
			// expo-api's admin listener on the internal network, and the shared
			// token it requires (expo-api's API_ADMIN_TOKEN).
			ADMIN_API_URL: envField.string({ context: "server", access: "secret" }),
			ADMIN_API_TOKEN: envField.string({ context: "server", access: "secret" }),
			// Identity-aware proxy: the team domain
			// (myteam.example.com) and the application's AUD tag. Both
			// set: every request must carry a valid signed JWT. Neither set: the
			// console refuses everyone, except ADMIN_DEV_USER on a local origin.
			ADMIN_ACCESS_TEAM_DOMAIN: envField.string({
				context: "server",
				access: "secret",
				optional: true,
			}),
			ADMIN_ACCESS_AUD: envField.string({
				context: "server",
				access: "secret",
				optional: true,
			}),
			// Development only: the staff name used instead of Access, honoured
			// only while Access is unset and ADMIN_ORIGIN is localhost.
			ADMIN_DEV_USER: envField.string({
				context: "server",
				access: "secret",
				optional: true,
			}),
			// The public site's origin, for links to reported entries
			// (e.g. https://factorio-expo.example); unset shows ids only.
			ADMIN_SITE_URL: envField.string({
				context: "server",
				access: "secret",
				optional: true,
			}),
			// Dozzle on the internal network, for the Overview's resource
			// usage (src/lib/dozzle.ts); unset hides it.
			ADMIN_DOZZLE_URL: envField.string({
				context: "server",
				access: "secret",
				optional: true,
			}),
			// The logs link in the sidebar (Dozzle); unset hides it.
			ADMIN_LOGS_URL: envField.string({
				context: "server",
				access: "secret",
				optional: true,
			}),
		},
	},
	vite: {
		resolve: {
			alias: {
				"@styles": "/src/styles",
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
	],
});
