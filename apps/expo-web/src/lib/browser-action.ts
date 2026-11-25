// Browser-only binding of lib/action.ts: the API client for this page and
// the runner every user-triggered API call goes through. Import only from
// client-side code.
import { getBrowserApiClient } from "@lib/api/browser";
import { performAction, type ActionOptions, type StatusLine } from "@lib/action";
import { performCheckedAction } from "@lib/checked-action";
import type { HumanCheck } from "@lib/turnstile";
import { loginHref, markSignedOut } from "@lib/auth";

export type { ActionOptions } from "@lib/action";

/** API client acting as this visitor: session cookie plus the URL's edit token. */
export const browserApi = () => getBrowserApiClient();

/** What a 401 does to this page (lib/action.ts SessionEffects). */
const session = {
	signedOut: markSignedOut,
	goToLogin: () => location.assign(loginHref()),
};

/**
 * Runs one user-triggered API call (lib/action.ts performAction): disables
 * the controls, shows the busy text, and reports any failure. Resolves to the
 * call's value, or undefined after a failure.
 */
export function runAction<T>(
	options: ActionOptions,
	call: () => Promise<T>,
): Promise<T | undefined> {
	return performAction(options, call, session);
}

/**
 * runAction behind a human check (lib/checked-action.ts): shows the check's
 * waiting text until it passes, hands the call its token and resets the check
 * after a failed attempt.
 */
export function runCheckedAction<T>(
	check: HumanCheck,
	options: ActionOptions & { status: StatusLine },
	call: (token: string) => Promise<T>,
): Promise<T | undefined> {
	return performCheckedAction(check, options, call, session);
}

/**
 * runAction for a toggle button (favorite, pin): no status line beside it,
 * so the button's tooltip and accessible description say what failed, and a
 * success clears that again. Resolves to whether the call succeeded.
 */
export async function runToggle(
	button: HTMLButtonElement,
	fallback: string,
	call: () => Promise<unknown>,
): Promise<boolean> {
	const saved = await runAction(
		{
			controls: [button],
			restore: true,
			fallback,
			onUnauthorized: "login",
			report: (message) => {
				button.dataset.tooltip = message;
				button.setAttribute("aria-description", message);
			},
		},
		() => call().then(() => true),
	);
	if (saved) button.removeAttribute("aria-description");
	return saved === true;
}

export { turnstileField } from "@lib/checked-action";
