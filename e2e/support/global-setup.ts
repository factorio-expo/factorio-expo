import { stackUp, waitForStack } from "./stack.ts";

// E2E_STACK: unset = fresh stack per run (torn down afterwards), "keep" = fresh
// stack left running, "reuse" = run against an already running stack.
export default async function globalSetup(): Promise<void> {
	if (process.env.E2E_STACK !== "reuse") stackUp();
	await waitForStack();
}
