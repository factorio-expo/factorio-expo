import { stackDown } from "./stack.ts";

export default async function globalTeardown(): Promise<void> {
	if (process.env.E2E_STACK === "reuse" || process.env.E2E_STACK === "keep") return;
	stackDown();
}
