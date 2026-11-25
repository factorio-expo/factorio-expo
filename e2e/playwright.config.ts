import { defineConfig, devices } from "@playwright/test";
import { webURL } from "./support/stack.ts";

// Black-box tests of the whole product through a browser. globalSetup starts a
// fresh e2e compose stack (see compose.yaml) unless E2E_STACK=reuse.
export default defineConfig({
	testDir: "./tests",
	globalSetup: "./support/global-setup.ts",
	globalTeardown: "./support/global-teardown.ts",
	fullyParallel: true,
	forbidOnly: Boolean(process.env.CI),
	retries: process.env.CI ? 1 : 0,
	reporter: process.env.CI
		? [["list"], ["html", { open: "never" }]]
		: [["list"], ["html", { open: "on-failure" }]],
	timeout: 60_000,
	expect: { timeout: 10_000 },
	use: {
		baseURL: webURL,
		locale: "en-GB",
		timezoneId: "UTC",
		colorScheme: "light",
		trace: "retain-on-failure",
		screenshot: "only-on-failure",
	},
	projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
