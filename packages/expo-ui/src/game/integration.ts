// The Astro integration both apps use: serves the game icon sprite sheets
// (written by expo-game-data into packages/expo-ui/game-icons) at
// /game-icons/ under `astro dev`, and copies them into the build's client
// output, so each app serves its own copy.
import { existsSync, readFileSync } from "node:fs";
import { cp } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import type { AstroIntegration } from "astro";

const SPRITES = fileURLToPath(new URL("../../game-icons/", import.meta.url));
const ROUTE = "/game-icons/";

export default function gameIcons(): AstroIntegration {
	let client: URL | undefined;
	return {
		name: "expo-ui-game-icons",
		hooks: {
			"astro:config:done": ({ config }) => {
				client = config.build.client;
			},
			"astro:server:setup": ({ server }) => {
				server.middlewares.use((request, response, next) => {
					const path = request.url?.split("?")[0] ?? "";
					const name = path.startsWith(ROUTE) ? path.slice(ROUTE.length) : "";
					if (!/^[a-z0-9-]+\.webp$/.test(name)) return next();
					const file = join(SPRITES, name);
					// Dev only, a few small files: a synchronous read keeps the handler simple.
					if (!existsSync(file)) return next();
					response.setHeader("Content-Type", "image/webp");
					response.end(readFileSync(file));
				});
			},
			"astro:build:done": async ({ logger }) => {
				if (!client) return;
				await cp(SPRITES, fileURLToPath(new URL("game-icons/", client)), {
					recursive: true,
				});
				logger.info("copied the game icon sprite sheets");
			},
		},
	};
}
