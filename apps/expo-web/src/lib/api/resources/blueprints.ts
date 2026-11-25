import type { BlueprintPayloadBody } from "@factorio-expo/api-types/public";
import { ok, type ApiClient } from "../http-client";

/**
 * The blueprint string tools (`POST /api/v1/blueprints/*`). They store nothing;
 * an invalid string or blueprint is a 422 with the reason. Also the re-render
 * of a full preview the storage budget evicted.
 */
export function createBlueprintsApi(http: ApiClient) {
	return {
		/** Exchange string to its JSON, as Factorio wrote it. */
		decode: (blueprint: string) =>
			ok(http.api.POST("/api/v1/blueprints/decode", { body: { blueprint } })).then(
				(response) => response.payload,
			),
		/** JSON to an exchange string; refused unless the result is a valid upload. */
		encode: (payload: BlueprintPayloadBody["payload"]) =>
			ok(http.api.POST("/api/v1/blueprints/encode", { body: { payload } })).then(
				(response) => response.blueprint,
			),
		/** The exchange string of any book, blueprint or planner the viewer may see. */
		exportNode: (nodeId: string) =>
			ok(
				http.api.GET("/api/v1/nodes/{nodeID}/export", {
					params: { path: { nodeID: nodeId } },
				}),
			),
		/** The upload form's summary of a string. */
		inspect: (blueprint: string, options?: { signal?: AbortSignal }) =>
			ok(
				http.api.POST("/api/v1/blueprints/inspect", {
					body: { blueprint },
					signal: options?.signal,
				}),
			),
		/** Asks for an evicted full preview to be rendered again (a no-op otherwise). */
		requestPreviewRender: (blueprintId: string) =>
			ok(
				http.api.POST("/api/v1/blueprints/{blueprintID}/preview/render", {
					params: { path: { blueprintID: blueprintId } },
				}),
			),
	};
}

export type BlueprintsApi = ReturnType<typeof createBlueprintsApi>;
