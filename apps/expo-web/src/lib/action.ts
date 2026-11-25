// The one runner for user-triggered API calls (the browser binds it in
// lib/browser-action.ts): disables the controls, shows the busy text, and on
// failure shows the error, applies the 401 policy and undoes what the caller
// asked. Free of DOM globals, so every rule here is unit tested.
import { ApiError } from "@lib/api/http-client";
import { explainApiError } from "@lib/api-errors";
import { setStatus, type StatusElement } from "@lib/status";

/**
 * What a call does when the API answers 401:
 * - "login": the page needs a session; go to /login.
 * - "clear": the page also works without one (anonymous edit tokens); show
 *   the page as logged out and show the error.
 * - "ignore": the 401 is not about the session (wrong credentials); show the error.
 */
export type UnauthorizedPolicy = "login" | "clear" | "ignore";

/** A status line: the element setStatus writes to. */
export type StatusLine = StatusElement;

/** Anything a call disables while it runs. */
export interface Control {
	disabled: boolean;
}

export interface ActionOptions {
	/** Where the busy text and a failure show. */
	status?: StatusLine;
	/** Shows a failure where there is no status line (a lone toggle button). */
	report?: (message: string) => void;
	/** Disabled while the call runs, re-enabled on failure. */
	controls?: Control[];
	/** Re-enables the controls after success too, for a page that stays. */
	restore?: boolean;
	/** Spinner text shown while the call runs. */
	busy?: string;
	/** The error text when the API gives none. */
	fallback: string;
	/** Texts for statuses with a fixed meaning here, e.g. 404: "Collection not found." */
	messages?: Partial<Record<number, string>>;
	onUnauthorized: UnauthorizedPolicy;
	/** Undoes an optimistic change (a toggled checkbox) when the call fails. */
	onFailure?: () => void;
}

/** What a 401 may do to the page; lib/browser-action.ts binds it to the browser. */
export interface SessionEffects {
	signedOut(): void;
	goToLogin(): void;
}

/**
 * The text a failure shows: the site's own text for a coded error
 * (lib/api-errors.ts), a fixed text for its status, the API's own text, or
 * `fallback`.
 */
export function failureText(
	err: unknown,
	fallback: string,
	messages: Partial<Record<number, string>> = {},
): string {
	if (!(err instanceof ApiError)) return fallback;
	return explainApiError(err) ?? messages[err.status] ?? err.detail ?? fallback;
}

/**
 * Runs one user-triggered API call. Resolves to the call's value, or
 * undefined after a failure (callers that need a value from a void call
 * return `true`). A 401 under the "login" policy leaves the page instead of
 * reporting.
 */
export async function performAction<T>(
	options: ActionOptions,
	call: () => Promise<T>,
	session: SessionEffects,
): Promise<T | undefined> {
	const { status, report, controls = [], busy } = options;
	const enable = (enabled: boolean) => {
		for (const control of controls) control.disabled = !enabled;
	};
	enable(false);
	if (status) setStatus(status, busy ?? "", Boolean(busy));
	try {
		const value = await call();
		// A page that stays drops the busy text; one that navigates keeps it.
		if (options.restore) {
			enable(true);
			if (status && busy) setStatus(status, "");
		}
		return value;
	} catch (err) {
		options.onFailure?.();
		enable(true);
		const unauthorized = err instanceof ApiError && err.status === 401;
		if (unauthorized && options.onUnauthorized !== "ignore") {
			session.signedOut();
			if (options.onUnauthorized === "login") {
				session.goToLogin();
				return undefined;
			}
		}
		const message = failureText(err, options.fallback, options.messages);
		if (status) setStatus(status, message);
		else report?.(message);
		return undefined;
	}
}
