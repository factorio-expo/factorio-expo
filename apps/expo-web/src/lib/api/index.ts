import { ApiClient, type ApiClientOptions } from "./http-client";
import { createAuthApi } from "./resources/auth";
import { createBlueprintsApi } from "./resources/blueprints";
import { createChallengesApi } from "./resources/challenges";
import { createCollectionsApi } from "./resources/collections";
import { createCompareApi } from "./resources/compare";
import { createCommentsApi } from "./resources/comments";
import { createEntriesApi } from "./resources/entries";
import { createFavoritesApi } from "./resources/favorites";
import { createLeaderboardsApi } from "./resources/leaderboards";
import { createModsetsApi } from "./resources/modsets";
import { createSearchApi } from "./resources/search";
import { createSiteApi } from "./resources/site";
import { createSitemapApi } from "./resources/sitemap";
import { createTagsApi } from "./resources/tags";
import { createUsersApi } from "./resources/users";

/**
 * Builds a domain-grouped API client bound to one base URL and one caller's
 * credentials. Construct a fresh instance per request (see server.ts /
 * browser.ts) rather than sharing one module-level instance, since the
 * token/edit-token differ per caller.
 */
export function createApiClient(options: ApiClientOptions) {
	const http = new ApiClient(options);
	return {
		entries: createEntriesApi(http),
		search: createSearchApi(http),
		site: createSiteApi(http),
		sitemap: createSitemapApi(http),
		auth: createAuthApi(http),
		blueprints: createBlueprintsApi(http),
		challenges: createChallengesApi(http),
		collections: createCollectionsApi(http),
		comments: createCommentsApi(http),
		compare: createCompareApi(http),
		favorites: createFavoritesApi(http),
		leaderboards: createLeaderboardsApi(http),
		modsets: createModsetsApi(http),
		tags: createTagsApi(http),
		users: createUsersApi(http),
	};
}

export type ApiClientInstance = ReturnType<typeof createApiClient>;

export { ApiError } from "./http-client";
export type { ApiClientOptions } from "./http-client";
export {
	blueprintRawPath,
	entryRawPath,
	revisionRawPath,
	shownRevisionRawPath,
	toPublicAssetUrl,
} from "./asset-url";
export { providerStartPath } from "./resources/auth";
export * from "./types";
