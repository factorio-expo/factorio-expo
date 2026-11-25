import { ok, type ApiClient } from "../http-client";

/** GET /api/v1/sitemap/entries (internal/httpapi/sitemap.go). */
export function createSitemapApi(http: ApiClient) {
	return {
		/** One page of listed entries in id order; pass the previous page's `next` as `after`. */
		entries: (after?: string) =>
			ok(http.api.GET("/api/v1/sitemap/entries", { params: { query: { after } } })),
	};
}

export type SitemapApi = ReturnType<typeof createSitemapApi>;
