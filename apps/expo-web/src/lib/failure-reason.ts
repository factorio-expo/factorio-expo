// Plain-language reasons for preview and analysis failure codes (the codes
// are the runner's and the API's: docs/architecture.md, docs/api.md). Unknown
// codes fall back to a generic sentence; the code itself is shown beside it.

const reasons: Record<string, string> = {
	invalid_blueprint: "The renderer could not read this blueprint.",
	blueprint_too_large: "The blueprint is too large to render.",
	request_too_large: "The blueprint is too large to render.",
	invalid_result: "The renderer returned a result that could not be read.",
	result_mismatch: "The renderer returned a result for a different blueprint.",
	analysis_failed: "The production analysis could not be completed.",
	render_failed: "The preview could not be rendered.",
	factorio_restarted: "The renderer stopped while processing this blueprint.",
};

/** A sentence for a failure code, or undefined for an unknown or empty code. */
export const failureReason = (code?: string) =>
	code && Object.hasOwn(reasons, code) ? reasons[code] : undefined;
