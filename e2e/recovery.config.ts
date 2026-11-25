import { defineConfig } from "@playwright/test";

// The recovery suite (recovery/): crashes and restarts Factorio on the e2e
// stack, so the stack runs with Factorio (factorio.compose.yaml) and the tests
// run one at a time. Same E2E_STACK lifecycle as playwright.config.ts.
process.env.E2E_FACTORIO = "1";

export default defineConfig({
	testDir: "./recovery",
	globalSetup: "./support/global-setup.ts",
	globalTeardown: "./support/global-teardown.ts",
	workers: 1,
	fullyParallel: false,
	forbidOnly: Boolean(process.env.CI),
	// Per test, hooks included: a cold client (no atlas cache) needs minutes to
	// join, and each recovery waits on Factorio's own timeouts.
	timeout: 12 * 60_000,
	reporter: process.env.CI
		? [["list"], ["html", { open: "never" }]]
		: [["list"], ["html", { open: "on-failure" }]],
	expect: { timeout: 10_000 },
	use: { trace: "off" },
});
