import { ok, type ApiClient } from "../http-client";
import type { Board } from "../types";

/** The ship record boards (`GET /api/v1/leaderboards`) and the home page's picks (`GET /api/v1/home-picks`). */
export function createLeaderboardsApi(http: ApiClient) {
	return {
		/** Every board with up to `limit` places each (hidden boards with none). */
		list: (limit?: number) =>
			ok(http.api.GET("/api/v1/leaderboards", { params: { query: { limit } } })),
		get: (board: Board, limit?: number) =>
			ok(
				http.api.GET("/api/v1/leaderboards/{board}", {
					params: { path: { board }, query: { limit } },
				}),
			),
		/** The blueprints staff picked for the home page, newest first. */
		homePicks: () => ok(http.api.GET("/api/v1/home-picks")),
	};
}

export type LeaderboardsApi = ReturnType<typeof createLeaderboardsApi>;
