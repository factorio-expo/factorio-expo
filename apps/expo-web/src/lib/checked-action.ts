// A user-triggered API call behind a human check (lib/turnstile.ts): the one
// place that holds the token rules. A check still running shows its waiting
// text instead of calling; a token is good for one request, so the check is
// reset after every failed attempt. Free of DOM globals (the browser binds it
// in lib/browser-action.ts), so it is unit tested with a fake check.
import {
	performAction,
	type ActionOptions,
	type SessionEffects,
	type StatusLine,
} from "@lib/action";
import { setStatus } from "@lib/status";
import type { HumanCheck } from "@lib/turnstile";

/** The request field carrying the token; nothing while the check is off (token ""). */
export function turnstileField(token: string): { turnstile_token?: string } {
	return token ? { turnstile_token: token } : {};
}

/**
 * performAction behind `check`. `call` gets the token ("" while the check is
 * off). Resolves to the call's value, or undefined when the check had not
 * passed (the waiting text is shown) or the call failed.
 */
export async function performCheckedAction<T>(
	check: HumanCheck,
	options: ActionOptions & { status: StatusLine },
	call: (token: string) => Promise<T>,
	session: SessionEffects,
): Promise<T | undefined> {
	const token = check.token();
	if (token === undefined) {
		setStatus(options.status, check.waiting());
		return undefined;
	}
	const result = await performAction(options, () => call(token), session);
	if (result === undefined) check.reset();
	return result;
}
