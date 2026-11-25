// The JSON shapes of apps/expo-api. The types of the operations in its
// OpenAPI document are generated (packages/expo-api-types) and re-exported
// here; the rest still mirror the Go types by hand until their routes move
// to Huma (see src/lib/api/README.md).
export type {
	BlueprintParameter,
	BlueprintSchedule,
	AnalysisBuildCost as BuildCost,
	CompareLeaf,
	CompareResponse,
	BlueprintDeconstructionSettings as DeconstructionSettings,
	BlueprintEntityStat as EntityStat,
	Entry,
	EntryChallenge,
	ProgressSnapshot as EntryProgress,
	AnalysisFootprint as Footprint,
	LeafDetail,
	ProgressLeaf as LeafProgress,
	LeafRevisionContext,
	AnalysisMaterialRate as MaterialRate,
	ModDetection,
	ModMatch,
	ModListed as Modset,
	Node,
	Note as BlueprintNote,
	NoteEditBody as NoteEdit,
	NoteList,
	ProgressChange as PhaseChange,
	ProgressPhaseState as PhaseState,
	BlueprintPlannerFilter as PlannerFilter,
	Preview,
	Production,
	EntryDoneEvent as ProgressDone,
	HistoryStep,
	EntryGoneEvent as ProgressGone,
	BlueprintRecipeStat as RecipeStat,
	Revision,
	BlueprintScheduleCondition as ScheduleCondition,
	BlueprintScheduleInterrupt as ScheduleInterrupt,
	BlueprintScheduleSignal as ScheduleSignal,
	BlueprintScheduleStop as ScheduleStop,
	BlueprintScheduleWaitCondition as ScheduleWaitCondition,
	SearchFacetCounts as FacetCounts,
	SearchResponse,
	SearchResultCard,
	BlueprintSnapToGrid as SnapToGrid,
	AnalysisSpacePlatform as SpacePlatform,
	Tag,
	TagGroup,
	BlueprintUpgradeMapping as UpgradeMapping,
	BlueprintUpgradeSettings as UpgradeSettings,
	Collection,
	CollectionMembership,
	EntrySummary,
	ProfileComment,
	ProfileStats,
	ProfileTag,
	PublicUser,
	SocialLink,
	AuthResponse,
	PasskeyAssertion,
	PasskeyAttestation,
	PendingSignupResponse as PendingSignup,
	ProviderResponse as SignInProvider,
	SessionResponse as UserSession,
	SignInMethodsResponse as SignInMethods,
	SignupRequest,
	SiteStatusResponse as SiteStatus,
	UserResponse,
	ChallengeDescription as ChallengeRules,
	ChallengeViewer,
	SitemapPage,
	Badge,
	BadgeRule,
	Board,
	BoardGroup,
	BoardPlaceBody as BoardPlace,
	BoardRecord,
	HeldRecord,
	HomePickBody as HomePick,
	LeaderboardBody as Leaderboard,
	UserProgress,
	XpAwardRule as XPAwardRule,
	XpLevel as XPLevel,
	XpReason as XPReason,
	XpRules as XPRules,
} from "@factorio-expo/api-types/public";
import type { ProfileRequest } from "@factorio-expo/api-types/public";
import type {
	Ban as GeneratedBan,
	Challenge as GeneratedChallenge,
	ChallengeDetail as GeneratedChallengeDetail,
	ChallengeEntry as GeneratedChallengeEntry,
	Comment as GeneratedComment,
	CommentPage as GeneratedCommentPage,
	CommentViewer as GeneratedCommentViewer,
	BlueprintDeconstructionSettings as DeconstructionSettings,
	Entry,
	FullRender,
	Inspection,
	LeafDetail,
	Node,
	NoteBody,
	Production,
	ProgressChange as PhaseChange,
	ProgressPhaseState as PhaseState,
	ReportProblemBody,
	operations,
} from "@factorio-expo/api-types/public";

/** The bug button's report: the generated body, with the path and the text required as the site sends them. */
export type ProblemReportInput = Omit<ReportProblemBody, "body" | "page"> &
	Required<Pick<ReportProblemBody, "body" | "page">>;

export type Visibility = Entry["visibility"];
export type NodeKind = Node["kind"];
export type PlannerKind = Exclude<NodeKind, "book" | "blueprint">;
export type PlannerFilterMode = DeconstructionSettings["entity_filter_mode"];
export type PreviewAvailability = LeafDetail["preview_status"];
export type ProgressPhase = PhaseChange["phase"];
export type ProgressStatus = PhaseState["status"];
export type ProductionAvailability = Production["availability"];
export type Compat21 = LeafDetail["compat_21"];
/** The query parameters of GET /api/v1/search, as the API takes them. */
type SearchParameters = NonNullable<operations["searchEntries"]["parameters"]["query"]>;
/** The windows trending counts views in (calendar periods, UTC). */
export type TrendingPeriod = NonNullable<
	NonNullable<operations["listTrending"]["parameters"]["query"]>["period"]
>;

export type SearchSort = NonNullable<SearchParameters["sort"]>;
export type Quality = NonNullable<SearchParameters["entity_quality"]>;

/** A note to pin: the generated NoteBody with every member required (the site always sends both pin fractions and the text). */
export type NewNote = Required<NoteBody>;

/** `POST .../preview/render` (Go `preview.FullRender`): the generated status is a plain string; narrowed to the progress phases. */
export type PreviewRender = Omit<FullRender, "status"> & { status: ProgressStatus };

/** GET /api/v1/sitemap/entries: listed entries in id order. */
/**
 * The parameters of GET /api/v1/search exactly as they are sent: lists
 * comma-separated, numeric filters as JSON (lib/search/query.ts builds them).
 */
export type SearchQuery = SearchParameters;

export interface SearchPlatformFilter {
	metric: "weight_tons" | "thrust_mn" | "max_speed_km_s";
	operator: ">" | "=" | "<";
	value: number;
}

export interface SearchProductionFilter {
	/** Fluids have no quality, so a fluid filter never carries one. */
	type: "item" | "fluid";
	product: string;
	quality?: string;
	/** The rate out or in, or the rate out per tile of footprint or per MW
	 * of electricity consumed (blueprints only). */
	direction: ProductionDirection;
	operator: ">" | "=" | "<";
	rate: number;
}

export type ProductionDirection = "out" | "in" | "out_per_tile" | "out_per_mw";

/** A raw resource of a blueprint's build cost compared to an amount. */
export interface SearchCostFilter {
	type: "item" | "fluid";
	resource: string;
	operator: ">" | "=" | "<";
	amount: number;
}

/**
 * The codes the site words itself (lib/api-errors.ts) or branches on; every
 * other code shows the problem's `detail` text.
 * - 503 `uploads_paused`, `anonymous_uploads_paused`, `signups_paused`,
 *   `comments_paused`: an action staff paused.
 * - 403 `turnstile_failed`, 503 `turnstile_unavailable`: the human check did
 *   not pass or could not run.
 * - 429 `render_queue_full`: the uploader's render queue is full.
 * - 422 `preview_failed`: a quick preview of a string that already failed.
 */
export type ExplainedErrorCode =
	| "uploads_paused"
	| "anonymous_uploads_paused"
	| "signups_paused"
	| "comments_paused"
	| "turnstile_failed"
	| "turnstile_unavailable"
	| "render_queue_full"
	| "preview_failed";

/** A site a profile links (Go `catalog.socialPlatforms`). */
export type SocialPlatform = "youtube" | "twitch" | "github" | "reddit" | "bluesky" | "steam" | "x";

/** A change to the caller's own profile; a missing member stays as it is. */
export type ProfileUpdate = Omit<ProfileRequest, "links"> & {
	/** Replaces the whole set. */
	links?: { platform: SocialPlatform; handle: string }[];
};

/** What a ban blocks (Go `catalog.BanAction`). */
export type BanAction = "comment" | "upload" | "report";

/** An active ban as the banned caller sees it (Go `catalog.Ban`). */
/**
 * An active ban as the banned caller sees it (Go `catalog.Ban`). The generated
 * action and reason_code are plain strings; narrowed to the API's sets here.
 */
export type Ban = Omit<GeneratedBan, "action" | "reason_code"> & {
	action: BanAction;
	reason_code: "spam" | "abuse" | "illegal_content" | "other";
};

/**
 * One entry comment (Go `catalog.Comment`); a removed one has no body. The
 * generated removed_by is a plain string; narrowed to the API's set here.
 */
export type Comment = Omit<GeneratedComment, "removed_by"> & {
	/** `account`: the author deleted their account. */
	removed_by?: "author" | "owner" | "admin" | "account";
};

/** Whether the viewer may post, and why not (Go `catalog.CommentViewer`). */
export type CommentViewer = Omit<GeneratedCommentViewer, "ban"> & {
	ban?: Ban;
};

/** One page of an entry's thread (Go `catalog.CommentPage`). */
export type CommentPage = Omit<GeneratedCommentPage, "comments" | "viewer"> & {
	comments: Comment[];
	viewer: CommentViewer;
};

export type CommentReportReason = "spam" | "abuse" | "off_topic" | "other";

/** Why an entry is reported (Go `catalog.entryReportReasons`); "stolen" needs a note. */
export type EntryReportReason = "stolen" | "offensive" | "spam" | "misleading" | "other";

/** `POST /api/v1/blueprints/inspect` (Go `catalog.Inspection`). */
export type BlueprintInspection = Inspection;

/** Derived from the times: draft is never public. */
export type ChallengeStatus = "draft" | "upcoming" | "open" | "judging" | "closed";

/** One challenge (catalog.Challenge). The generated status is a plain string; narrowed here. */
export type Challenge = Omit<GeneratedChallenge, "status"> & {
	status: ChallengeStatus;
};

export type ChallengeEntryStatus = "pending" | "accepted" | "rejected";

/** One entry on the challenge page (catalog.ChallengeEntry); its status narrowed here. */
export type ChallengeEntry = Omit<GeneratedChallengeEntry, "status"> & {
	status: ChallengeEntryStatus;
};

/** The challenge page (catalog.ChallengeDetail). */
export type ChallengeDetail = Omit<GeneratedChallengeDetail, "status" | "entries"> & {
	status: ChallengeStatus;
	entries: ChallengeEntry[];
};
