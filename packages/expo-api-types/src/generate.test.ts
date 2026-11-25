import { readFile } from "node:fs/promises";
import { expect, test } from "vitest";
import { generate, listeners, type Listener } from "./generate.ts";

for (const listener of Object.keys(listeners) as Listener[]) {
	test(`${listener}.d.ts matches the OpenAPI document (run pnpm generate)`, async () => {
		expect(await readFile(listeners[listener].output, "utf8")).toBe(await generate(listener));
	});
}
