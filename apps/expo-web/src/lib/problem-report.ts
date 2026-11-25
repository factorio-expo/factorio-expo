// The bug button's page context (components/layout/ProblemReport.astro):
// what an entry page marks with ProblemContext.astro, plus the window, as the
// report the API takes and as rows the dialog shows before sending.
import type { ProblemReportInput } from "@lib/api";

/** What ProblemContext.astro writes into data-problem-context. */
export interface PageProblemContext {
	entry_id?: string;
	blueprint_id?: string;
	context?: Record<string, string>;
}

export type ProblemReportContext = Omit<ProblemReportInput, "body" | "email" | "turnstile_token">;

const LABELS: Record<string, string> = {
	revision: "Revision",
	game_version: "Game version",
	preview_status: "Preview",
	preview_failure: "Preview failure",
	analysis: "Analysis",
	viewport: "Window size",
	theme: "Theme",
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
	typeof value === "object" && value !== null && !Array.isArray(value);

/**
 * The report's context: the page path, the entry page's marker (`raw`, its
 * JSON; ignored when unreadable) and `extra` facts. Empty values are left out.
 */
export function problemContextOf(
	page: string,
	raw: string | undefined,
	extra: Record<string, string>,
): ProblemReportContext {
	let marked: unknown;
	try {
		marked = raw ? JSON.parse(raw) : undefined;
	} catch {
		marked = undefined;
	}
	const report: ProblemReportContext = { page };
	const context: Record<string, string> = {};
	if (isRecord(marked)) {
		if (typeof marked.entry_id === "string" && marked.entry_id)
			report.entry_id = marked.entry_id;
		if (typeof marked.blueprint_id === "string" && marked.blueprint_id)
			report.blueprint_id = marked.blueprint_id;
		if (isRecord(marked.context)) {
			for (const [key, value] of Object.entries(marked.context)) {
				if (typeof value === "string" && value) context[key] = value;
			}
		}
	}
	for (const [key, value] of Object.entries(extra)) if (value) context[key] = value;
	if (Object.keys(context).length > 0) report.context = context;
	return report;
}

/** The context as label/value rows, in a stable order. */
export function problemContextRows(report: ProblemReportContext): [string, string][] {
	const rows: [string, string][] = [["Page", report.page]];
	if (report.entry_id) rows.push(["Entry", report.entry_id]);
	if (report.blueprint_id) rows.push(["Blueprint", report.blueprint_id]);
	for (const [key, value] of Object.entries(report.context ?? {})) {
		rows.push([LABELS[key] ?? key, value]);
	}
	return rows;
}
