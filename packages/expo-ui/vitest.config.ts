import { defineConfig } from "vitest/config";

// Unit tests cover pure modules only (no DOM, no Astro runtime).
export default defineConfig({ test: { include: ["src/**/*.test.ts"] } });
