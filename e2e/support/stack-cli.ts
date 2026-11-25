// `pnpm stack:up|down|logs`: manage the e2e stack by hand (e.g. to run tests
// repeatedly with E2E_STACK=reuse).
import { compose, stackDown, stackUp, waitForStack, webURL } from "./stack.ts";

const command = process.argv[2];
if (command === "up") {
	stackUp();
	await waitForStack();
	console.log(`e2e stack is up at ${webURL}`);
} else if (command === "down") {
	stackDown();
} else if (command === "logs") {
	compose("logs", "--follow", ...process.argv.slice(3));
} else {
	console.error("usage: node support/stack-cli.ts up|down|logs [service...]");
	process.exit(2);
}
