import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const src = (path: string) => fileURLToPath(new URL(`./src/${path}`, import.meta.url));

// Unit tests cover pure modules only (no DOM, no Astro runtime).
export default defineConfig({
	resolve: {
		alias: {
			"@components": src("components"),
			"@layouts": src("layouts"),
			"@lib": src("lib"),
			"@utils": src("lib/utils"),
			"@data": src("data"),
		},
	},
	test: { include: ["src/**/*.test.ts"] },
});
