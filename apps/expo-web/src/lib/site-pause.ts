// What staff paused from the console (docs/admin.md#settings): the texts pages
// and forms show, also for the API's coded 503 refusals (lib/api-errors.ts).
// Pure; the server-side loader is lib/site-status.ts.
import type { SiteStatus } from "@lib/api/types";

/** Everything open: what pages assume while the API cannot say otherwise. */
export const OPEN_SITE: SiteStatus = {
	banner: "",
	uploads: true,
	anonymous_uploads: true,
	signups: true,
	comments: true,
};

export const UPLOADS_PAUSED = "Uploads are paused right now. Please try again later.";
export const ANONYMOUS_UPLOADS_PAUSED =
	"Uploads without an account are paused right now. Log in to upload, or try again later.";
export const SIGNUPS_PAUSED = "New sign-ups are paused right now. Please try again later.";
export const COMMENTS_PAUSED = "Comments are paused right now. Please try again later.";

/** Why this viewer cannot upload (or preview, or add a revision) right now, if they cannot. */
export function uploadPause(site: SiteStatus, signedIn: boolean): string | undefined {
	if (!site.uploads) return UPLOADS_PAUSED;
	if (!signedIn && !site.anonymous_uploads) return ANONYMOUS_UPLOADS_PAUSED;
	return undefined;
}
