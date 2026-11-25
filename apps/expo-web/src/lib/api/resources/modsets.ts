import { ok, type ApiClient } from "../http-client";

/** GET /api/v1/modsets: the modsets mod detection knows beyond the game's own mods. */
export function createModsetsApi(http: ApiClient) {
	return {
		list: () => ok(http.api.GET("/api/v1/modsets")),
	};
}

export type ModsetsApi = ReturnType<typeof createModsetsApi>;
