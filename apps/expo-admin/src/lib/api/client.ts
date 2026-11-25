import { ADMIN_API_TOKEN, ADMIN_API_URL } from "astro:env/server";
import createClient from "openapi-fetch";
import type { paths } from "@factorio-expo/api-types/admin";
import { fetchWithTimeout, ok } from "@factorio-expo/ui/api/problem.ts";

import type {
	ChallengeFields,
	StaffTagRule,
	TagFields,
	BanEnd,
	JobKind,
	NewBan,
	ProblemStatus,
	ReporterBan,
	StaffEntryQuery,
	UserStatus,
} from "./types";

const TIMEOUT_MS = 10_000;
/** Cleanups delete in batches and may take a while. */
const LONG_TIMEOUT_MS = 120_000;

// The staff member a request is for (expo-api's staff.UserHeader).
const USER_HEADER = "X-Staff-User";

/** A form's non-empty values only, as the API's optional query parameters. */
const present = (value: string | undefined) => (value ? value : undefined);

/** The staff API, called for one staff member (their Access email). */
export function adminApi(staffUsername: string) {
	const client = (timeoutMs: number) =>
		createClient<paths>({
			baseUrl: ADMIN_API_URL.replace(/\/$/, ""),
			headers: {
				Authorization: `Bearer ${ADMIN_API_TOKEN}`,
				[USER_HEADER]: staffUsername,
			},
			fetch: fetchWithTimeout(timeoutMs),
		});
	const http = client(TIMEOUT_MS);
	const slow = client(LONG_TIMEOUT_MS);
	return {
		overview: () => ok(http.GET("/admin/overview")),
		badges: () => ok(http.GET("/admin/badges")),
		audit: (cursor?: string) =>
			ok(
				http.GET("/admin/audit", {
					params: { query: { cursor: present(cursor) } },
				}),
			),

		entryReports: () => ok(http.GET("/admin/reports/entries")).then((r) => r.groups),
		resolveEntryReports: (entryId: string, reason: string) =>
			ok(
				http.POST("/admin/reports/entries/{entryID}/resolve", {
					params: { path: { entryID: entryId } },
					body: { reason },
				}),
			),
		banEntryReporter: (reportId: string, ban: ReporterBan) =>
			ok(
				http.POST("/admin/entry-reports/{reportID}/ban-reporter", {
					params: { path: { reportID: reportId } },
					body: ban,
				}),
			),
		deleteEntry: (entryId: string, reason: string) =>
			ok(
				http.POST("/admin/entries/{entryID}/delete", {
					params: { path: { entryID: entryId } },
					body: { reason },
				}),
			),

		commentReports: () => ok(http.GET("/admin/reports/comments")).then((r) => r.groups),
		resolveCommentReports: (commentId: string, reason: string) =>
			ok(
				http.POST("/admin/reports/comments/{commentID}/resolve", {
					params: { path: { commentID: commentId } },
					body: { reason },
				}),
			),
		removeComment: (commentId: string, reason: string) =>
			ok(
				http.POST("/admin/comments/{commentID}/remove", {
					params: { path: { commentID: commentId } },
					body: { reason },
				}),
			),

		problemReports: (status: "open" | "resolved") =>
			ok(http.GET("/admin/problem-reports", { params: { query: { status } } })),
		resolveProblemReport: (reportId: string, status: ProblemStatus, note: string) =>
			ok(
				http.POST("/admin/problem-reports/{reportID}/resolve", {
					params: { path: { reportID: reportId } },
					body: { status, note },
				}),
			),
		setProblemReportNote: (reportId: string, note: string) =>
			ok(
				http.POST("/admin/problem-reports/{reportID}/note", {
					params: { path: { reportID: reportId } },
					body: { note },
				}),
			),
		banProblemReporter: (reportId: string, ban: ReporterBan) =>
			ok(
				http.POST("/admin/problem-reports/{reportID}/ban-reporter", {
					params: { path: { reportID: reportId } },
					body: ban,
				}),
			),

		bans: (username: string) =>
			ok(
				http.GET("/admin/bans", {
					params: { query: { username: present(username) } },
				}),
			),
		createBan: (ban: NewBan) => ok(http.POST("/admin/bans", { body: ban })),
		changeBanEnd: (banId: string, end: BanEnd) =>
			ok(
				http.POST("/admin/bans/{banID}/end", {
					params: { path: { banID: banId } },
					body: end,
				}),
			),
		revokeBan: (banId: string, reason: string) =>
			ok(
				http.POST("/admin/bans/{banID}/revoke", {
					params: { path: { banID: banId } },
					body: { reason },
				}),
			),
		ipBans: () => ok(http.GET("/admin/ip-bans")).then((r) => r.bans),
		liftIPBan: (hash: string, action: string, reason: string) =>
			ok(
				http.POST("/admin/ip-bans/{hash}/lift", {
					params: { path: { hash } },
					body: { action, reason },
				}),
			),

		entries: (query: StaffEntryQuery) =>
			ok(
				http.GET("/admin/entries", {
					params: {
						query: {
							q: present(query.q),
							state: present(query.state),
							visibility: present(query.visibility),
							author: present(query.author),
							failed: query.failed ? true : undefined,
							mods: present(query.mods),
							from: present(query.from),
							to: present(query.to),
							min_leaves: query.min_leaves ? Number(query.min_leaves) : undefined,
							cursor: present(query.cursor),
						},
					},
				}),
			),
		entry: (entryId: string) =>
			ok(
				http.GET("/admin/entries/{entryID}", {
					params: { path: { entryID: entryId } },
				}),
			),
		revisionString: (entryId: string, revisionId: string) =>
			ok(
				http.GET("/admin/entries/{entryID}/revisions/{revisionID}/raw", {
					params: { path: { entryID: entryId, revisionID: revisionId } },
					parseAs: "text",
				}),
			),
		restoreEntry: (entryId: string, reason: string) =>
			ok(
				http.POST("/admin/entries/{entryID}/restore", {
					params: { path: { entryID: entryId } },
					body: { reason },
				}),
			),
		reindexEntry: (entryId: string, reason: string) =>
			ok(
				http.POST("/admin/entries/{entryID}/reindex", {
					params: { path: { entryID: entryId } },
					body: { reason },
				}),
			),
		purgeEntryCache: (entryId: string, reason: string) =>
			ok(
				http.POST("/admin/entries/{entryID}/purge-cache", {
					params: { path: { entryID: entryId } },
					body: { reason },
				}),
			),
		/** Re-runs a phase of one blueprint, or of every blueprint of the current revision. */
		rerunEntry: (
			entryId: string,
			rerun: { phase: "preview" | "analysis"; blueprint_id?: string; reason: string },
		) =>
			ok(
				http.POST("/admin/entries/{entryID}/rerun", {
					params: { path: { entryID: entryId } },
					body: rerun,
				}),
			),

		tags: () => ok(http.GET("/admin/tags")),
		createTagGroup: (fields: TagFields) => ok(http.POST("/admin/tag-groups", { body: fields })),
		updateTagGroup: (groupId: string, fields: TagFields) =>
			ok(
				http.POST("/admin/tag-groups/{groupID}", {
					params: { path: { groupID: groupId } },
					body: fields,
				}),
			),
		deleteTagGroup: (groupId: string) =>
			ok(
				http.POST("/admin/tag-groups/{groupID}/delete", {
					params: { path: { groupID: groupId } },
				}),
			),
		createTag: (fields: TagFields) => ok(http.POST("/admin/tags", { body: fields })),
		updateTag: (tagId: string, fields: TagFields) =>
			ok(
				http.POST("/admin/tags/{tagID}", {
					params: { path: { tagID: tagId } },
					body: fields,
				}),
			),
		deleteTag: (tagId: string) =>
			ok(
				http.POST("/admin/tags/{tagID}/delete", {
					params: { path: { tagID: tagId } },
				}),
			),
		setTagRule: (tagId: string, rule: StaffTagRule) =>
			ok(
				http.POST("/admin/tags/{tagID}/rules", {
					params: { path: { tagID: tagId } },
					body: { ...rule, max_count: rule.max_count ?? undefined },
				}),
			),
		deleteTagRule: (tagId: string, kind: string, name: string) =>
			ok(
				http.POST("/admin/tags/{tagID}/rules/delete", {
					params: { path: { tagID: tagId } },
					body: { kind, name },
				}),
			),
		reservedUsernames: () => ok(http.GET("/admin/reserved-usernames")).then((r) => r.names),
		reserveUsername: (name: string, reason: string) =>
			ok(http.POST("/admin/reserved-usernames", { body: { name, reason } })),
		releaseUsername: (name: string) =>
			ok(
				http.POST("/admin/reserved-usernames/{name}/delete", {
					params: { path: { name } },
				}),
			),

		homePicks: () => ok(http.GET("/admin/home-picks")).then((r) => r.picks),
		addHomePick: (blueprintId: string, caption: string) =>
			ok(
				http.POST("/admin/home-picks", {
					body: { blueprint_id: blueprintId, caption },
				}),
			),
		setHomePickCaption: (blueprintId: string, caption: string) =>
			ok(
				http.POST("/admin/home-picks/{blueprintID}", {
					params: { path: { blueprintID: blueprintId } },
					body: { caption },
				}),
			),
		removeHomePick: (blueprintId: string) =>
			ok(
				http.POST("/admin/home-picks/{blueprintID}/delete", {
					params: { path: { blueprintID: blueprintId } },
				}),
			),

		challenges: () => ok(http.GET("/admin/challenges")).then((r) => r.challenges),
		challengeParams: () => ok(http.GET("/admin/challenges/params")).then((r) => r.params),
		challenge: (challengeId: string) =>
			ok(
				http.GET("/admin/challenges/{challengeID}", {
					params: { path: { challengeID: challengeId } },
				}),
			),
		/** Creates a draft; answers its id. */
		createChallenge: (fields: ChallengeFields) =>
			ok(http.POST("/admin/challenges", { body: fields })).then((r) => r.id),
		updateChallenge: (challengeId: string, fields: ChallengeFields) =>
			ok(
				http.POST("/admin/challenges/{challengeID}", {
					params: { path: { challengeID: challengeId } },
					body: fields,
				}),
			),
		publishChallenge: (challengeId: string, published: boolean) =>
			ok(
				http.POST(
					published
						? "/admin/challenges/{challengeID}/publish"
						: "/admin/challenges/{challengeID}/unpublish",
					{ params: { path: { challengeID: challengeId } } },
				),
			),
		deleteChallenge: (challengeId: string) =>
			ok(
				http.POST("/admin/challenges/{challengeID}/delete", {
					params: { path: { challengeID: challengeId } },
				}),
			),
		challengeOversight: (challengeId: string) =>
			ok(
				http.GET("/admin/challenges/{challengeID}/oversight", {
					params: { path: { challengeID: challengeId } },
				}),
			),
		/** Voids (or, with `void: false`, restores) a voter's votes; no entry: all of them. */
		voidVotes: (
			challengeId: string,
			vote: { userId: string; entryId?: string; void: boolean; reason: string },
		) =>
			ok(
				http.POST(
					vote.void
						? "/admin/challenges/{challengeID}/votes/void"
						: "/admin/challenges/{challengeID}/votes/unvoid",
					{
						params: { path: { challengeID: challengeId } },
						body: {
							user_id: vote.userId,
							entry_id: present(vote.entryId),
							reason: vote.reason,
						},
					},
				),
			).then((r) => r.changed),
		/** Runs a pasted blueprint as a test; resolves to the quick preview's entry id. */
		startChallengeTest: (challengeId: string, exchangeString: string) =>
			ok(
				http.POST("/admin/challenges/{challengeID}/test", {
					params: { path: { challengeID: challengeId } },
					body: { exchange_string: exchangeString },
				}),
			).then((r) => r.entry_id),
		challengeTest: (challengeId: string, entryId: string) =>
			ok(
				http.GET("/admin/challenges/{challengeID}/test/{entryID}", {
					params: { path: { challengeID: challengeId, entryID: entryId } },
				}),
			),
		setSubmissionVerdict: (
			challengeId: string,
			submissionId: string,
			verdict: "" | "accepted" | "rejected",
			reason: string,
		) =>
			ok(
				http.POST("/admin/challenges/{challengeID}/submissions/{submissionID}/verdict", {
					params: { path: { challengeID: challengeId, submissionID: submissionId } },
					body: { verdict, reason },
				}),
			),

		users: (query: { q?: string; status?: UserStatus; cursor?: string }) =>
			ok(
				http.GET("/admin/users", {
					params: {
						query: {
							q: present(query.q),
							status: present(query.status),
							cursor: present(query.cursor),
						},
					},
				}),
			),
		user: (userId: string) =>
			ok(
				http.GET("/admin/users/{userID}", {
					params: { path: { userID: userId } },
				}),
			),
		deletionQueue: () => ok(http.GET("/admin/users/deletion-queue")).then((r) => r.accounts),
		logoutUser: (userId: string, reason: string) =>
			ok(
				http.POST("/admin/users/{userID}/logout", {
					params: { path: { userID: userId } },
					body: { reason },
				}),
			),
		clearUserProfile: (userId: string, reason: string) =>
			ok(
				http.POST("/admin/users/{userID}/clear-profile", {
					params: { path: { userID: userId } },
					body: { reason },
				}),
			),
		deleteUser: (userId: string, reason: string) =>
			ok(
				http.POST("/admin/users/{userID}/delete", {
					params: { path: { userID: userId } },
					body: { reason },
				}),
			),

		failedJobs: () =>
			ok(http.GET("/admin/jobs/failed", { params: { query: {} } })).then((r) => r.jobs),
		rerunJob: (kind: JobKind, targetId: string, reason: string) =>
			ok(
				http.POST("/admin/jobs/{kind}/{targetID}/rerun", {
					params: { path: { kind, targetID: targetId } },
					body: { reason },
				}),
			),

		settings: () => ok(http.GET("/admin/settings")).then((r) => r.settings),
		/** Sets settings by key; answers the keys that changed. */
		updateSettings: (values: Record<string, string>, reason: string) =>
			ok(http.POST("/admin/settings", { body: { values, reason } })).then((r) => r.changed),

		cleanups: (targetBytes?: number) =>
			ok(
				http.GET("/admin/cleanups", {
					params: {
						query: {
							target_bytes:
								targetBytes === undefined ? undefined : String(targetBytes),
						},
					},
				}),
			),
		runCleanup: (name: string, reason: string, targetBytes?: number) =>
			ok(
				slow.POST("/admin/cleanups/{name}", {
					params: { path: { name } },
					body: { reason, target_bytes: targetBytes },
				}),
			),
	};
}

export type AdminApi = ReturnType<typeof adminApi>;
