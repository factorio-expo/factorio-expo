// The API client: `api`, the typed openapi-fetch client of the operations in
// the API's OpenAPI document (@factorio-expo/api-types). Errors, `ok` and the
// timeout are shared with the staff console (@factorio-expo/ui/api/problem.ts).
// See src/lib/api/README.md.
import createClient, { type Client } from "openapi-fetch";
import type { paths } from "@factorio-expo/api-types/public";
import { fetchWithTimeout } from "@factorio-expo/ui/api/problem.ts";

export { ApiError, apiErrorCode, ok } from "@factorio-expo/ui/api/problem.ts";

/** The header the API reads an anonymous entry's edit token from (httpapi.EditTokenInput). */
export const EDIT_TOKEN_HEADER = "X-Edit-Token";

export interface ApiClientOptions {
	/** Origin the client talks to, e.g. "http://expo-api:8080". No trailing slash required. */
	baseUrl: string;
	/**
	 * Bearer token, SSR only: the server lifts it from the browser's HttpOnly
	 * session cookie. Browser code never sees the token; it sends the cookie.
	 */
	token?: string;
	/**
	 * Anonymous edit token for one entry: sent as the X-Edit-Token header on
	 * every request (operations that do not take one ignore it), so it stays
	 * out of logged URLs.
	 */
	editToken?: string;
	/** "include" in the browser so the session cookie travels with requests. */
	credentials?: RequestCredentials;
	/** Extra headers on every request (SSR forwards the visitor's address). */
	headers?: Record<string, string>;
	/** Gives up on a request after this long (SSR: a hung API must not stall page renders). */
	timeoutMs?: number;
}

/**
 * One caller's typed client: base URL, bearer (SSR) / cookie (`credentials`)
 * and edit-token auth, extra headers, timeout. Unwrap a call with `ok`.
 */
export class ApiClient {
	/** The typed client of the operations in the OpenAPI document; unwrap a call with `ok`. */
	readonly api: Client<paths>;

	constructor(options: ApiClientOptions) {
		this.api = createClient<paths>({
			baseUrl: options.baseUrl.replace(/\/+$/, ""),
			credentials: options.credentials,
			headers: options.headers,
			fetch: options.timeoutMs ? fetchWithTimeout(options.timeoutMs) : undefined,
		});
		this.api.use({
			onRequest: ({ request }) => {
				if (options.token) request.headers.set("Authorization", `Bearer ${options.token}`);
				if (options.editToken) request.headers.set(EDIT_TOKEN_HEADER, options.editToken);
				return request;
			},
		});
	}
}
