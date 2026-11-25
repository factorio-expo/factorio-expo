import type { CollectionRequest } from "@factorio-expo/api-types/public";
import { ok, type ApiClient } from "../http-client";

/** Endpoints under /api/v1/collections and /api/v1/entries/{id}/collections (internal/httpapi/collections.go). */
export function createCollectionsApi(http: ApiClient) {
	return {
		create: (input: CollectionRequest) =>
			ok(http.api.POST("/api/v1/collections", { body: input })),

		listMine: () => ok(http.api.GET("/api/v1/collections")),

		/** The caller's collections, each flagged with whether it holds the entry. */
		listForEntry: (entryId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/collections", {
					params: { path: { entryID: entryId } },
				}),
			),

		get: (collectionId: string) =>
			ok(
				http.api.GET("/api/v1/collections/{collectionID}", {
					params: { path: { collectionID: collectionId } },
				}),
			),

		update: (collectionId: string, input: CollectionRequest) =>
			ok(
				http.api.PATCH("/api/v1/collections/{collectionID}", {
					params: { path: { collectionID: collectionId } },
					body: input,
				}),
			),

		delete: (collectionId: string) =>
			ok(
				http.api.DELETE("/api/v1/collections/{collectionID}", {
					params: { path: { collectionID: collectionId } },
				}),
			),

		addEntry: (collectionId: string, entryId: string, position?: number) =>
			ok(
				http.api.POST("/api/v1/collections/{collectionID}/entries", {
					params: { path: { collectionID: collectionId } },
					body: { entry_id: entryId, position },
				}),
			),

		/** Stores the owner's order of the collection's entries. */
		reorderEntries: (collectionId: string, entryIds: string[]) =>
			ok(
				http.api.PUT("/api/v1/collections/{collectionID}/entries", {
					params: { path: { collectionID: collectionId } },
					body: { entry_ids: entryIds },
				}),
			),

		removeEntry: (collectionId: string, entryId: string) =>
			ok(
				http.api.DELETE("/api/v1/collections/{collectionID}/entries/{entryID}", {
					params: { path: { collectionID: collectionId, entryID: entryId } },
				}),
			),
	};
}

export type CollectionsApi = ReturnType<typeof createCollectionsApi>;
