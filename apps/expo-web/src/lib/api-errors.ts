// The texts for API errors (Go `httpapi.writeError`): every error body is
// application/problem+json (Problem), and `detail` is already written for people,
// so a form shows it as it is. This registry holds only the codes the site
// words its own way: a paused action (lib/site-pause.ts), a failed human
// check, and the render queue's refusals
// (docs/architecture.md#render-queue). performAction uses it for every form.
import { ApiError } from "@lib/api/http-client";
import type { ExplainedErrorCode } from "@lib/api/types";
import { failureReason } from "@lib/failure-reason";
import {
	ANONYMOUS_UPLOADS_PAUSED,
	COMMENTS_PAUSED,
	SIGNUPS_PAUSED,
	UPLOADS_PAUSED,
} from "@lib/site-pause";

export const RENDER_QUEUE_FULL =
	"You have too many blueprints waiting to render (see the fair use policy). Try again once some of them have finished.";

const explainers: Record<ExplainedErrorCode, (err: ApiError) => string> = {
	uploads_paused: () => UPLOADS_PAUSED,
	anonymous_uploads_paused: () => ANONYMOUS_UPLOADS_PAUSED,
	signups_paused: () => SIGNUPS_PAUSED,
	comments_paused: () => COMMENTS_PAUSED,
	turnstile_failed: () => "The check that you are human did not pass. Try again.",
	turnstile_unavailable: () =>
		"The check that you are human is not working right now. Try again in a few minutes.",
	render_queue_full: () => RENDER_QUEUE_FULL,
	preview_failed: (err) => {
		const reason = failureReason(err.fields.failure_code);
		return `This blueprint already failed to render, so it was not queued again${reason ? `: ${reason}` : "."}`;
	},
};

const isExplained = (code: string | undefined): code is ExplainedErrorCode =>
	code !== undefined && Object.hasOwn(explainers, code);

/** The site's own text for a coded API error, if it words that code itself. */
export function explainApiError(err: unknown): string | undefined {
	if (!(err instanceof ApiError) || !isExplained(err.code)) return undefined;
	return explainers[err.code](err);
}
