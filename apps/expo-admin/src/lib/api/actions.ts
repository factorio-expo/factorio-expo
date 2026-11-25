import type { AstroGlobal } from "astro";

import { outcomeUrl } from "../forms";
import { adminApi, type AdminApi } from "./client";
import { ApiError } from "@factorio-expo/ui/api/problem.ts";

/**
 * Handles a page's form post (post/redirect/get): runs `handle` with the
 * staff member's API and the form, then sends the browser back to the page
 * with its message as ?notice=, or what went wrong as ?error=. Returns
 * undefined for a GET, so the page renders as usual.
 *
 * `handle` returns the notice to show, or throws a FormError for input the
 * console itself refuses.
 */
export async function handlePost(
	Astro: AstroGlobal,
	handle: (api: AdminApi, form: FormData) => Promise<string>,
): Promise<Response | undefined> {
	if (Astro.request.method !== "POST") return undefined;
	let form: FormData;
	try {
		form = await Astro.request.formData();
	} catch {
		return Astro.redirect(outcomeUrl(Astro.url, "error", "The form could not be read."), 303);
	}
	try {
		const notice = await handle(adminApi(Astro.locals.staff.username), form);
		return Astro.redirect(outcomeUrl(Astro.url, "notice", notice), 303);
	} catch (error) {
		return Astro.redirect(outcomeUrl(Astro.url, "error", failureMessage(error)), 303);
	}
}

/** Input the console refuses before asking the API. */
export class FormError extends Error {
	constructor(message: string) {
		super(message);
		this.name = "FormError";
	}
}

/** What a failed staff write shows: the console's or the API's own message, or a retry hint. */
export function failureMessage(error: unknown): string {
	if (error instanceof FormError) return error.message;
	if (error instanceof ApiError) {
		if (error.status === 404)
			return "It is gone or already handled. The page shows it as it is now.";
		if (error.status < 500)
			return error.invalid ? `${error.message} (${error.invalid})` : error.message;
	}
	console.error("staff write failed", error);
	return "The API did not answer. Try again in a moment.";
}
