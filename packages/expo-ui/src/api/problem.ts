// The expo-api's answers as both frontends read them. Every error body is
// application/problem+json (Go `httpapi.Problem`), and every call is a typed
// openapi-fetch call, unwrapped with `ok`. The two apps keep only their base
// URL and header policy (expo-web lib/api/http-client.ts, expo-admin
// lib/api/client.ts).

/** A non-2xx answer: its status and the problem read from its body. */
export class ApiError extends Error {
	readonly status: number;
	readonly statusText: string;
	readonly body: string;
	/** The problem's `code`, when the body is the API's problem+json. */
	readonly code: string | undefined;
	/** The problem's `detail`: the text written for people. */
	readonly detail: string | undefined;
	/** A validation problem's invalid fields, technical ("body.title: expected length <= 160"): for staff, never for the public. */
	readonly invalid: string | undefined;
	/** The problem's extra string fields (preview_failed's failure_code). */
	readonly fields: Readonly<Record<string, string>>;

	/**
	 * The message is the text for people: the problem's detail, else the
	 * status text (a proxy's own error text is not shown; it stays in `body`).
	 */
	constructor(status: number, statusText: string, body: string) {
		const problem = parseProblem(body);
		super(problem.detail || statusText);
		this.name = "ApiError";
		this.status = status;
		this.statusText = statusText;
		this.body = body;
		this.code = problem.code;
		this.detail = problem.detail;
		this.invalid = problem.invalid;
		this.fields = problem.fields;
	}
}

/** The problem+json members every error has; the rest are the error's own fields. */
const problemMembers = new Set(["type", "title", "status", "detail", "code", "instance"]);

/** Reads the API's problem+json body; anything else (a proxy's page) leaves it all undefined. */
export function parseProblem(body: string): {
	isProblem: boolean;
	code: string | undefined;
	detail: string | undefined;
	invalid: string | undefined;
	fields: Record<string, string>;
} {
	const fields: Record<string, string> = {};
	const none = {
		isProblem: false,
		code: undefined,
		detail: undefined,
		invalid: undefined,
		fields,
	};
	let parsed: unknown;
	try {
		parsed = JSON.parse(body);
	} catch {
		return none;
	}
	if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return none;
	const problem: Record<string, unknown> = { ...parsed };
	for (const [key, value] of Object.entries(problem)) {
		if (typeof value === "string" && !problemMembers.has(key)) fields[key] = value;
	}
	const code = typeof problem.code === "string" ? problem.code : undefined;
	const detail = typeof problem.detail === "string" ? problem.detail.trim() : "";
	const invalid = validationMessages(problem.errors);
	return {
		isProblem: true,
		code,
		detail: detail || undefined,
		invalid: invalid || undefined,
		fields,
	};
}

/** A validation_failed problem's errors as one line: "body.title: expected length <= 160". */
function validationMessages(errors: unknown): string {
	if (!Array.isArray(errors)) return "";
	const messages: string[] = [];
	for (const error of errors) {
		if (typeof error !== "object" || error === null) continue;
		const { message, location }: { message?: unknown; location?: unknown } = error;
		if (typeof message !== "string") continue;
		messages.push(
			typeof location === "string" && location ? `${location}: ${message}` : message,
		);
	}
	return messages.join("; ");
}

/** What a typed call resolves to (openapi-fetch's FetchResponse). */
interface TypedResult {
	data?: unknown;
	error?: unknown;
	response: Response;
}

/**
 * The data of a typed call; a non-2xx answer throws ApiError. A call whose
 * answer has no body (204) resolves to `never`, which any `void` caller
 * accepts.
 */
export async function ok<R extends TypedResult>(call: Promise<R>): Promise<NonNullable<R["data"]>> {
	const { data, error, response } = await call;
	if (!response.ok) {
		const body = typeof error === "string" ? error : JSON.stringify(error ?? "");
		throw new ApiError(response.status, response.statusText, body === '""' ? "" : body);
	}
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- a 2xx answer is the success branch, whose data the generated types describe
	return data as NonNullable<R["data"]>;
}

/** The `code` of an API error, if any. */
export function apiErrorCode(err: unknown): string | undefined {
	return err instanceof ApiError ? err.code : undefined;
}

/**
 * A fetch for openapi-fetch's `createClient` that gives up after `ms` (a hung
 * API must not stall a page render), keeping any signal the call passed.
 */
export function fetchWithTimeout(ms: number): (request: Request) => Promise<Response> {
	return (request) =>
		fetch(request, { signal: AbortSignal.any([request.signal, AbortSignal.timeout(ms)]) });
}
