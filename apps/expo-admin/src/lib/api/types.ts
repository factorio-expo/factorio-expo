// The staff API's types, generated from expo-api's OpenAPI document
// (apps/expo-api/openapi/admin.json) into @factorio-expo/api-types/admin.
// Re-export them under the names the console uses. Only the closed string sets
// the OpenAPI document leaves as plain strings, and the console's own URL-query
// types, are written by hand (each says why).

import type { components, operations } from "@factorio-expo/api-types/admin";

type Schemas = components["schemas"];

export type Overview = Schemas["AdminOverviewResponse"];
export type JobCounts = Schemas["JobCounts"];
export type AdminAction = Schemas["AdminActionResponse"];
export type AuditPage = Schemas["AuditPageResponse"];
export type Badges = Schemas["BadgesResponse"];
export type Reporter = Schemas["Reporter"];
export type ReportedEntry = Schemas["ReportedEntry"];
export type EntryReport = Schemas["StaffEntryReport"];
export type EntryReportGroup = Schemas["EntryReportGroup"];
export type ReportedComment = Schemas["ReportedComment"];
export type CommentReport = Schemas["StaffCommentReport"];
export type CommentReportGroup = Schemas["CommentReportGroup"];
export type ReporterBan = Schemas["ReporterBanBody"];
export type StaffBan = Schemas["StaffBan"];
export type Bans = Schemas["BansResponse"];
export type NewBan = Schemas["NewBanBody"];
export type BanEnd = Schemas["BanEndBody"];
export type IPBan = Schemas["StaffIPBan"];
export type CleanupStatus = Schemas["CleanupStatus"];
export type CleanupOverview = Schemas["CleanupOverview"];
export type CleanupResult = Schemas["CleanupResult"];
export type StaffSetting = Schemas["StaffSetting"];
export type StaffUser = Schemas["StaffUser"];
export type StaffUserPage = Schemas["StaffUserPage"];
export type StaffUserEntry = Schemas["StaffUserEntry"];
export type StaffUserDetail = Schemas["StaffUserDetail"];
export type StaffEntry = Schemas["StaffEntry"];
export type StaffEntryPage = Schemas["StaffEntryPage"];
export type StaffRevision = Schemas["StaffRevision"];
export type StaffLeaf = Schemas["StaffLeaf"];
export type StaffReport = Schemas["StaffReport"];
export type StaffComment = Schemas["StaffComment"];
export type StaffEntryDetail = Schemas["StaffEntryDetail"];
export type DeletedAccount = Schemas["DeletedAccount"];
export type FailedJob = Schemas["JobFailed"];
export type ProblemReport = Schemas["StaffProblemReport"];
export type ProblemReportPage = Schemas["ProblemReportPage"];
export type StaffTagGroup = Schemas["StaffTagGroup"];
export type StaffTagRule = Schemas["StaffTagRule"];
export type StaffTag = Schemas["StaffTag"];
export type StaffTags = Schemas["StaffTags"];
export type TagFields = Schemas["StaffTagFieldsBody"];
export type ReservedUsername = Schemas["ReservedUsername"];
export type StaffHomePick = Schemas["StaffHomePick"];
export type ChallengeTarget = Schemas["ChallengeTarget"];
export type ChallengeRestriction = Schemas["ChallengeRestriction"];
export type ChallengeRules = Schemas["ChallengeRules"];
export type ChallengeImportRule = Schemas["ChallengeImportRule"];
export type ChallengeParam = Schemas["ChallengeParam"];
export type ChallengeFields = Schemas["AdminChallengeFields"];
export type StaffChallenge = Schemas["StaffChallenge"];
export type StaffChallengeEntry = Schemas["StaffChallengeEntry"];
export type StaffChallengeDetail = Schemas["StaffChallengeDetail"];
export type VoteOversight = Schemas["VoteOversight"];
export type OversightVoter = Schemas["OversightVoter"];
export type OversightEntry = Schemas["OversightEntry"];
export type ChallengeTestResult = Schemas["ChallengeTestResult"];

/** The Failed jobs page's kind filter, from the generated query type. */
export type JobKind = NonNullable<
	NonNullable<operations["listFailedJobs"]["parameters"]["query"]>["kind"]
>;

// The OpenAPI document declares these as plain strings, so they stay hand-written
// closed sets (the generated types cannot narrow them).

/** The ban actions (the generated NewBanBody.actions is string[]). */
export type BanAction = "comment" | "upload" | "report";
/** The ban reason codes (the generated reason_code is a string). */
export type BanReason = "spam" | "abuse" | "illegal_content" | "other";
/** The Entries page's state tabs (the generated state query is a string). */
export type EntryState = "all" | "live" | "deleted" | "quick_preview";
/** How staff close a problem report (the generated close body's status is a string). */
export type ProblemStatus = "fixed" | "wont_fix" | "duplicate" | "spam";
/** A setting's kind (the generated StaffSetting.kind is a string). */
export type SettingKind = "bool" | "int" | "string";
/** The Users page's status tabs (the generated status query is a string). */
export type UserStatus = "all" | "active" | "banned" | "deleted";
/** A challenge's status (the generated status is a string). */
export type ChallengeStatus = "draft" | "upcoming" | "open" | "judging" | "closed";

// The console's own URL-query type: every field is a string as it comes from the
// URL. The client converts it to the generated query types (failed -> boolean,
// min_leaves -> number).
export interface StaffEntryQuery {
	q?: string;
	state?: EntryState;
	visibility?: string;
	author?: string;
	failed?: "1";
	mods?: string;
	from?: string;
	to?: string;
	min_leaves?: string;
	cursor?: string;
}
