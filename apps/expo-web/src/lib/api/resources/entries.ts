import type {
	EntryMetadataUpdate,
	PublishBody,
	ReportEntryBody,
	RevisionRequest,
	SubmissionRequest,
} from "@factorio-expo/api-types/public";
import { ok, type ApiClient } from "../http-client";
import type { NewNote, NoteEdit, TrendingPeriod, Visibility } from "../types";

/** What publishing a quick preview or anonymous entry sets (`POST /api/v1/entries/{id}/publish`). */
export type PublishInput = Required<Pick<PublishBody, "title" | "description">> &
	Partial<PublishBody> & {
		/** Required: the entry's visibility when published. */
		visibility: Visibility;
	};

/** The body of an upload (`POST /api/v1/entries`). */
export type SubmissionInput = SubmissionRequest & {
	/** Unlisted and private need a signed-in uploader; absent is public. */
	visibility?: Visibility;
};

/** The body of a new revision (`POST /api/v1/entries/{id}/revisions`): the string and an optional note. */
export type RevisionInput = RevisionRequest;

/** `POST /api/v1/entries/{id}/report` (Go `catalog.EntryReport`); a `stolen` report needs a note. */
export type EntryReportInput = Required<Pick<ReportEntryBody, "reason">> & Partial<ReportEntryBody>;

/** Endpoints under /api/v1/entries and /api/v1/blueprints, plus /api/v1/me/entries and /api/v1/previews (routes.go). */
export function createEntriesApi(http: ApiClient) {
	return {
		/** The most viewed listed entries of a calendar period (`GET /api/v1/trending`). */
		trending: (period: TrendingPeriod, limit?: number) =>
			ok(http.api.GET("/api/v1/trending", { params: { query: { period, limit } } })),
		create: (input: SubmissionInput) => ok(http.api.POST("/api/v1/entries", { body: input })),

		/**
		 * Fetches the blueprint behind a link to a supported site (`POST /api/v1/imports/resolve`);
		 * `consent` must be true, `turnstileToken` is required when signed out. The answer holds the
		 * exchange string, its inspection and the source token the upload sends back; nothing is stored.
		 */
		resolveImport: (link: string, consent: boolean, turnstileToken?: string) =>
			ok(
				http.api.POST("/api/v1/imports/resolve", {
					body: { link, consent, turnstile_token: turnstileToken || undefined },
				}),
			),

		/** A quick preview: one blueprint, unlisted, expires unless published. */
		createPreview: (blueprint: string, turnstileToken?: string) =>
			ok(
				http.api.POST("/api/v1/previews", {
					body: { blueprint, turnstile_token: turnstileToken },
				}),
			),

		/** Gives a finished quick preview or an anonymous entry (edit token) to the caller. */
		publish: (entryId: string, input: PublishInput) =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/publish", {
					params: { path: { entryID: entryId } },
					body: input,
				}),
			),

		createRevision: (entryId: string, input: RevisionInput) =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/revisions", {
					params: { path: { entryID: entryId } },
					body: input,
				}),
			),

		get: (entryId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}", {
					params: { path: { entryID: entryId } },
				}),
			),

		/** The signed-in caller's own entries, public and private, newest first. */
		listMine: (options: { limit?: number; offset?: number } = {}) =>
			ok(http.api.GET("/api/v1/me/entries", { params: { query: options } })),

		permissions: (entryId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/permissions", {
					params: { path: { entryID: entryId } },
				}),
			),

		update: (entryId: string, input: EntryMetadataUpdate) =>
			ok(
				http.api.PATCH("/api/v1/entries/{entryID}", {
					params: { path: { entryID: entryId } },
					body: input,
				}),
			),

		delete: (entryId: string) =>
			ok(
				http.api.DELETE("/api/v1/entries/{entryID}", {
					params: { path: { entryID: entryId } },
				}),
			),

		/** Reports the entry to the admins, signed in or not; `revision_id` is the one viewed. */
		report: (entryId: string, input: EntryReportInput) =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/report", {
					params: { path: { entryID: entryId } },
					body: input,
				}),
			),

		listRevisions: (entryId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/revisions", {
					params: { path: { entryID: entryId } },
				}),
			),

		getRevision: (entryId: string, revisionId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/revisions/{revisionID}", {
					params: { path: { entryID: entryId, revisionID: revisionId } },
				}),
			),

		/** Deletes a revision; the 200 answer names the revision promoted to live, a 204 leaves `undefined`. */
		deleteRevision: (entryId: string, revisionId: string) =>
			ok(
				http.api.DELETE("/api/v1/entries/{entryID}/revisions/{revisionID}", {
					params: {
						path: { entryID: entryId, revisionID: revisionId },
					},
				}),
			),

		exportRevision: (entryId: string, revisionId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/revisions/{revisionID}/export", {
					params: { path: { entryID: entryId, revisionID: revisionId } },
				}),
			),

		getLeafDetail: (entryId: string, blueprintId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/blueprints/{blueprintID}", {
					params: { path: { entryID: entryId, blueprintID: blueprintId } },
				}),
			),

		/** The entry's single-blueprint revisions with their renders, oldest first (the viewer's timeline). */
		history: (entryId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/history", {
					params: { path: { entryID: entryId } },
				}),
			),

		/** A leaf's notes, and whether the caller may change them (owner or edit token, live revision). */
		listNotes: (entryId: string, blueprintId: string) =>
			ok(
				http.api.GET("/api/v1/entries/{entryID}/blueprints/{blueprintID}/notes", {
					params: {
						path: { entryID: entryId, blueprintID: blueprintId },
					},
				}),
			),

		/** Pins a note; `x` and `y` are fractions (0..1) of the preview image. */
		createNote: (entryId: string, blueprintId: string, input: NewNote) =>
			ok(
				http.api.POST("/api/v1/entries/{entryID}/blueprints/{blueprintID}/notes", {
					params: {
						path: { entryID: entryId, blueprintID: blueprintId },
					},
					body: input,
				}),
			),

		/** Changes a note's text, moves its pin (`x` and `y` together), or both. */
		editNote: (entryId: string, blueprintId: string, noteId: string, change: NoteEdit) =>
			ok(
				http.api.PATCH(
					"/api/v1/entries/{entryID}/blueprints/{blueprintID}/notes/{noteID}",
					{
						params: {
							path: { entryID: entryId, blueprintID: blueprintId, noteID: noteId },
						},
						body: change,
					},
				),
			),

		deleteNote: (entryId: string, blueprintId: string, noteId: string) =>
			ok(
				http.api.DELETE(
					"/api/v1/entries/{entryID}/blueprints/{blueprintID}/notes/{noteID}",
					{
						params: {
							path: { entryID: entryId, blueprintID: blueprintId, noteID: noteId },
						},
					},
				),
			),
	};
}

export type EntriesApi = ReturnType<typeof createEntriesApi>;
