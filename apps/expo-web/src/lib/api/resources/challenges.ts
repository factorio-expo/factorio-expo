import { ok, type ApiClient } from "../http-client";

/** Design challenges under /api/v1/challenges (internal/httpapi/challenges.go). */
export function createChallengesApi(http: ApiClient) {
	return {
		list: () => ok(http.api.GET("/api/v1/challenges")),
		get: (slug: string) =>
			ok(http.api.GET("/api/v1/challenges/{slug}", { params: { path: { slug } } })),
		/** Enters one of the caller's public single-blueprint entries, replacing their earlier one. */
		submit: (slug: string, entryId: string) =>
			ok(
				http.api.POST("/api/v1/challenges/{slug}/entry", {
					params: { path: { slug } },
					body: { entry_id: entryId },
				}),
			),
		/** The caller's public single blueprints (newest first) whose title contains `q`. */
		candidates: (slug: string, options: { q?: string; limit?: number; offset?: number } = {}) =>
			ok(
				http.api.GET("/api/v1/challenges/{slug}/candidates", {
					params: { path: { slug }, query: options },
				}),
			),
		/** Takes the caller's entry out of the challenge. */
		withdraw: (slug: string) =>
			ok(http.api.DELETE("/api/v1/challenges/{slug}/entry", { params: { path: { slug } } })),
		vote: (slug: string, entryId: string) =>
			ok(
				http.api.PUT("/api/v1/challenges/{slug}/votes/{entryID}", {
					params: { path: { slug, entryID: entryId } },
				}),
			),
		unvote: (slug: string, entryId: string) =>
			ok(
				http.api.DELETE("/api/v1/challenges/{slug}/votes/{entryID}", {
					params: { path: { slug, entryID: entryId } },
				}),
			),
	};
}

export type ChallengesApi = ReturnType<typeof createChallengesApi>;
