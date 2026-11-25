import { ok, type ApiClient } from "../http-client";
import type { SearchQuery } from "../types";

/**
 * GET /api/v1/search (internal/httpapi/search.go). The query is already in
 * its wire form (lib/search/query.ts toApiQuery).
 */
export function createSearchApi(http: ApiClient) {
	return {
		query: (query: SearchQuery = {}) =>
			ok(http.api.GET("/api/v1/search", { params: { query } })),
	};
}

export type SearchApi = ReturnType<typeof createSearchApi>;
