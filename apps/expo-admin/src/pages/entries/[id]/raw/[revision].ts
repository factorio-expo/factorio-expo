// A revision's original exchange string as plain text, whatever the entry's
// state (the public raw routes serve live entries only).
import type { APIRoute } from "astro";

import { adminApi } from "@lib/api/client";
import { ApiError } from "@factorio-expo/ui/api/problem.ts";

export const GET: APIRoute = async ({ params, locals }) => {
	try {
		const raw = await adminApi(locals.staff.username).revisionString(
			params.id ?? "",
			params.revision ?? "",
		);
		return new Response(raw, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
	} catch (error) {
		if (error instanceof ApiError && error.status === 404) {
			return new Response("Not found", { status: 404 });
		}
		console.error("revision string", error);
		return new Response("The API did not answer.", { status: 502 });
	}
};
