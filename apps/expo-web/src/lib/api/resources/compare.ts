import { ok, type ApiClient } from "../http-client";

/** GET /api/v1/compare: two public blueprints with the analysis the compare page tabulates. */
export function createCompareApi(http: ApiClient) {
	return {
		leaves: (a: string, b: string) =>
			ok(http.api.GET("/api/v1/compare", { params: { query: { a, b } } })),
	};
}

export type CompareApi = ReturnType<typeof createCompareApi>;
