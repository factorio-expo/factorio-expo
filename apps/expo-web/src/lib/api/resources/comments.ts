import { ok, type ApiClient } from "../http-client";
import type { CommentReportReason } from "../types";

/** Endpoints under /api/v1/entries/{entryID}/comments (internal/httpapi/comments.go). */
export function createCommentsApi(http: ApiClient) {
	return {
		/** One page: after a number (oldest first, the default) or before one. */
		list: (
			entryId: string,
			options: { after?: number; before?: number; limit?: number } = {},
		) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/comments", {
					params: {
						path: { entryID: entryId },
						query: options,
					},
				}),
			),
		/** `revisionId`: the revision the author was looking at; absent is the live one. */
		create: (entryId: string, body: string, revisionId?: string) =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/comments", {
					params: { path: { entryID: entryId } },
					body: { body, revision_id: revisionId },
				}),
			),
		edit: (entryId: string, number: number, body: string) =>
			ok(
				http.api.PATCH("/api/v1/entries/{entryID}/comments/{number}", {
					params: { path: { entryID: entryId, number } },
					body: { body },
				}),
			),
		/** Leaves a "removed" tombstone (author, or the entry owner / edit-token holder). */
		remove: (entryId: string, number: number) =>
			ok(
				http.api.DELETE("/api/v1/entries/{entryID}/comments/{number}", {
					params: { path: { entryID: entryId, number } },
				}),
			),
		report: (entryId: string, number: number, reason: CommentReportReason, note = "") =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/comments/{number}/report", {
					params: { path: { entryID: entryId, number } },
					body: { reason, note },
				}),
			),
	};
}

export type CommentsApi = ReturnType<typeof createCommentsApi>;
