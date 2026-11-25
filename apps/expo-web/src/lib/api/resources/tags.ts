import { ok, type ApiClient } from "../http-client";

/** GET /api/v1/tags — the curated, fixed discovery taxonomy, grouped. */
export function createTagsApi(http: ApiClient) {
	return {
		list: () => ok(http.api.GET("/api/v1/tags")),
	};
}

export type TagsApi = ReturnType<typeof createTagsApi>;
