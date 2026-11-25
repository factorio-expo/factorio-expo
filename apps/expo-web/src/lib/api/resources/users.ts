import { ok, type ApiClient } from "../http-client";
import type { ProfileUpdate } from "../types";

type Page = { limit?: number; offset?: number };

/** Public profile endpoints under /api/v1/users/{username}, and the caller's own profile and pins. */
export function createUsersApi(http: ApiClient) {
	return {
		get: (username: string) =>
			ok(http.api.GET("/api/v1/users/{username}", { params: { path: { username } } })),
		entries: (username: string, options: Page = {}) =>
			ok(
				http.api.GET("/api/v1/users/{username}/entries", {
					params: { path: { username }, query: options },
				}),
			),
		collections: (username: string) =>
			ok(
				http.api.GET("/api/v1/users/{username}/collections", {
					params: { path: { username } },
				}),
			),
		/** The user's pinned entries, in their order. */
		pins: (username: string) =>
			ok(http.api.GET("/api/v1/users/{username}/pins", { params: { path: { username } } })),
		comments: (username: string, options: Page = {}) =>
			ok(
				http.api.GET("/api/v1/users/{username}/comments", {
					params: { path: { username }, query: options },
				}),
			),
		/** The entry behind a vanity alias (`/@{username}/{slug}`); 404 when it has none. */
		aliasTarget: (username: string, slug: string) =>
			ok(
				http.api.GET("/api/v1/users/{username}/aliases/{slug}", {
					params: { path: { username, slug } },
				}),
			),
		/** The data of the user's Atom feed: the last 30 publications and revisions. */
		feed: (username: string) =>
			ok(http.api.GET("/api/v1/users/{username}/feed", { params: { path: { username } } })),
		updateProfile: (update: ProfileUpdate) =>
			ok(http.api.PATCH("/api/v1/me/profile", { body: update })),
		/** Replaces the caller's pins with these entries, in this order. */
		setPins: (entryIds: string[]) =>
			ok(http.api.PUT("/api/v1/me/pins", { body: { entry_ids: entryIds } })),
		pin: (entryId: string) =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/pin", {
					params: { path: { entryID: entryId } },
				}),
			),
		unpin: (entryId: string) =>
			ok(
				http.api.DELETE("/api/v1/entries/{entryID}/pin", {
					params: { path: { entryID: entryId } },
				}),
			),
	};
}

export type UsersApi = ReturnType<typeof createUsersApi>;
