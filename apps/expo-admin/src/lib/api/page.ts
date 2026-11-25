import type { AstroGlobal } from "astro";

import { adminApi, type AdminApi } from "./client";
import { ApiError } from "@factorio-expo/ui/api/problem.ts";

/** What a page loaded, or the message to show instead. */
export type Loaded<T> = { data: T; error?: undefined } | { data?: undefined; error: string };

/**
 * Loads a page's data as the signed-in staff member. A missing item (404)
 * or an API that refuses or does not answer become a message with the
 * matching status.
 */
export async function loadFromApi<T>(
	Astro: AstroGlobal,
	load: (api: AdminApi) => Promise<T>,
): Promise<Loaded<T>> {
	try {
		return { data: await load(adminApi(Astro.locals.staff.username)) };
	} catch (error) {
		if (error instanceof ApiError && error.status === 404) {
			Astro.response.status = 404;
			return { error: "Not found: it may have been removed." };
		}
		if (error instanceof ApiError && error.status === 401) {
			console.error("staff API refused the console's token (API_ADMIN_TOKEN)");
			Astro.response.status = 502;
			return { error: "The API refused the console's token: check API_ADMIN_TOKEN." };
		}
		console.error("staff API call failed", Astro.url.pathname, error);
		Astro.response.status = 502;
		return { error: "The API did not answer. Try again in a moment." };
	}
}
