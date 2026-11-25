import { describe, expect, it } from "vitest";

import type { ProblemReport } from "./api/types";
import { problemIssueMarkdown } from "./problem-issue";

const report: ProblemReport = {
	id: "r1",
	body: "The preview is cut off.\n\nOn the right.",
	email: "me@example.org",
	page: "/e/abc",
	entry_id: "abc",
	blueprint_id: "",
	context: { game_version: "2.0.72", viewport: "800|600" },
	user_agent: "Firefox",
	created_at: "2026-11-09T10:00:00Z",
	status: "open",
	staff_note: "",
	resolved_at: null,
	resolved_by: "",
	reporter: { anonymous: false, username: "alice" },
};

describe("problemIssueMarkdown", () => {
	it("quotes the report and lists the page's facts", () => {
		const markdown = problemIssueMarkdown(report, "https://x.test/e/abc");
		expect(markdown).toContain("> The preview is cut off.\n>\n> On the right.");
		expect(markdown).toContain("| Page | https://x.test/e/abc |");
		expect(markdown).toContain(String.raw`| viewport | 800\|600 |`);
		expect(markdown).toContain("| Reported | 2026-11-09 |");
	});

	it("never names the reporter or their address", () => {
		const markdown = problemIssueMarkdown(report);
		expect(markdown).not.toContain("alice");
		expect(markdown).not.toContain("me@example.org");
	});
});
