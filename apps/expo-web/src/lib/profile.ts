// SSR loading shared by the public profile's tab pages (/u/{name}/...): the
// profile itself, whether the viewer owns it, and the text for link previews.
import type { AstroGlobal } from "astro";
import { ApiError, type PublicUser } from "@lib/api";
import { getServerApiClient } from "@lib/api/server.ts";
import { getViewer } from "@lib/viewer";
import { stripRichText } from "@factorio-expo/ui/game/rich-text.ts";

export interface LoadedProfile {
	user: PublicUser;
	owner: boolean;
	api: ReturnType<typeof getServerApiClient>;
	/** Plain text for search results and link previews. */
	description: string;
}

/** The profile named in the URL, or undefined when there is no such user. */
export async function loadProfile(astro: AstroGlobal): Promise<LoadedProfile | undefined> {
	const viewer = await getViewer(astro);
	const api = getServerApiClient(astro);
	let user: PublicUser;
	try {
		user = await api.users.get(astro.params.username!);
	} catch (err) {
		if (err instanceof ApiError && err.status === 404) return undefined;
		throw err;
	}
	const status = stripRichText(user.status).trim();
	return {
		user,
		owner: viewer.state === "in" && viewer.user.user_id === user.user_id,
		api,
		description: status
			? `${user.username} on Factorio Expo: ${status}`
			: `Factorio blueprints and collections shared by ${user.username} on Factorio Expo.`,
	};
}

/** This page's URL at another list offset (0 drops the parameter). */
export function offsetHref(url: URL, next: number): string {
	const target = new URL(url);
	if (next > 0) target.searchParams.set("offset", String(next));
	else target.searchParams.delete("offset");
	return target.pathname + target.search;
}

/** The ?offset= of this page's list. */
export function listOffset(url: URL): number {
	return Math.max(0, Number.parseInt(url.searchParams.get("offset") ?? "0", 10) || 0);
}
