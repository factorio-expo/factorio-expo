import { ok, type ApiClient } from "../http-client";

/** Endpoints under /api/v1/entries/{id}/favorite and /api/v1/me/favorites. */
export function createFavoritesApi(http: ApiClient) {
	return {
		/** Whether the signed-in caller favorited the entry (pages without the full entry read). */
		status: (entryId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/favorite", {
					params: { path: { entryID: entryId } },
				}),
			),
		add: (entryId: string) =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/favorite", {
					params: { path: { entryID: entryId } },
				}),
			),
		remove: (entryId: string) =>
			ok(
				http.api.DELETE("/api/v1/entries/{entryID}/favorite", {
					params: { path: { entryID: entryId } },
				}),
			),
		listMine: (options: { limit?: number; offset?: number } = {}) =>
			ok(http.api.GET("/api/v1/me/favorites", { params: { query: options } })),
	};
}

export type FavoritesApi = ReturnType<typeof createFavoritesApi>;
