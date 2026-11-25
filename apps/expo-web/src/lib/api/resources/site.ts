import { ok, type ApiClient } from "../http-client";
import type { ProblemReportInput } from "../types";

/** GET /api/v1/site: the site banner and which actions are paused; the bug button's report; the XP rules. */
export function createSiteApi(http: ApiClient) {
	return {
		status: () => ok(http.api.GET("/api/v1/site")),
		/** `GET /api/v1/xp-rules`: the points, daily caps, levels and badge criteria the code awards. */
		xpRules: () => ok(http.api.GET("/api/v1/xp-rules")),
		/** `POST /api/v1/problem-reports`: anyone, signed in or not. */
		reportProblem: (input: ProblemReportInput) =>
			ok(http.api.POST("/api/v1/problem-reports", { body: input })),
	};
}

export type SiteApi = ReturnType<typeof createSiteApi>;
