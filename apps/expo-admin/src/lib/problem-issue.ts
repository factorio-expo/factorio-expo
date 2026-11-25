// A problem report as a GitHub issue body (the console's "As a GitHub issue"
// box). Never the reporter or their contact address: the repository is public.
import type { ProblemReport } from "./api/types";

/** Markdown for an issue: the report's text quoted, then what the page knew. */
export function problemIssueMarkdown(report: ProblemReport, pageUrl?: string): string {
	const quoted = report.body
		.split("\n")
		.map((line) => (line ? `> ${line}` : ">"))
		.join("\n");
	const facts: [string, string][] = [];
	if (report.page) facts.push(["Page", pageUrl ?? report.page]);
	if (report.entry_id) facts.push(["Entry", report.entry_id]);
	if (report.blueprint_id) facts.push(["Blueprint", report.blueprint_id]);
	for (const [key, value] of Object.entries(report.context)) facts.push([key, value]);
	if (report.user_agent) facts.push(["Browser", report.user_agent]);
	facts.push(["Reported", report.created_at.slice(0, 10)]);
	const table = facts.map(
		([name, value]) => `| ${name} | ${value.replaceAll("|", String.raw`\|`)} |`,
	);
	return [quoted, "", "| | |", "|---|---|", ...table, ""].join("\n");
}
