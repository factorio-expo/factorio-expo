// Browser-only session helpers. The session is an HttpOnly cookie that scripts
// cannot read (so XSS cannot steal it), and the browser stores nothing else
// about it: the server renders who is viewing (`data-auth` on <html>, the
// header's account link; see lib/viewer.ts). A 401 is the only way a page
// learns the session ended under it (see lib/browser-action.ts). Import only
// from client-side code.
import { getBrowserApiClient } from "@lib/api/browser";
import { loginPath, safeNext as safeNextFor } from "@lib/safe-next";

const SIGNED_OUT_EVENT = "expo:signed-out";

/** Whether the server rendered this page for a signed-in visitor, and no call has since been refused. */
export const isSignedIn = () => document.documentElement.hasAttribute("data-auth");

/** The session ended under this page (a 401): show it as logged out without reloading. */
export function markSignedOut() {
	const root = document.documentElement;
	if (!root.hasAttribute("data-auth")) return;
	root.removeAttribute("data-auth");
	window.dispatchEvent(new Event(SIGNED_OUT_EVENT));
}

/** Runs `callback` once if this page is marked signed out (see `markSignedOut`). */
export function onSignedOut(callback: () => void) {
	window.addEventListener(SIGNED_OUT_EVENT, callback, { once: true });
}

/** Logs this device out: the API ends its session and clears the HttpOnly cookie. */
export async function logout() {
	await getBrowserApiClient().auth.logout();
}

/** Same-origin post-login destination; see lib/safe-next.ts. */
export const safeNext = (raw: string | null | undefined) => safeNextFor(raw, location.origin);

export function loginHref(next = location.pathname + location.search) {
	return loginPath(next);
}
