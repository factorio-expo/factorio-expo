// Cloudflare Turnstile, the human check on the two sign-up forms (/signup,
// a provider's username step, and the login page's "I'm new here" passkey
// form) and on the forms signed-out visitors submit work with: upload, an import
// from a link, new revision (edit link), quick preview, entry report and the bug button's
// problem report (docs/frontend.md#turnstile). It is on only when PUBLIC_TURNSTILE_SITE_KEY
// is set at build time, and only those pages allow Cloudflare's script and
// frame in their CSP (allowTurnstile, allowGuestTurnstile). The widget hands
// the form a token, good for one request; the API verifies it with the
// form's action (Go `turnstileAction*`) and the site's host name.
import type { AstroGlobal } from "astro";

export const TURNSTILE_ORIGIN = "https://challenges.cloudflare.com";
const SCRIPT_URL = `${TURNSTILE_ORIGIN}/turnstile/v0/api.js?render=explicit`;

/** One action per form: the API accepts a token only for its own form. */
export const TURNSTILE_ACTIONS = [
	"signup-provider",
	"signup-passkey",
	"upload",
	"import",
	"revision",
	"quick-preview",
	"report",
	"problem-report",
] as const;
export type TurnstileAction = (typeof TURNSTILE_ACTIONS)[number];

const isAction = (value: string | undefined): value is TurnstileAction =>
	(TURNSTILE_ACTIONS as readonly (string | undefined)[]).includes(value);

/**
 * Lets the page being rendered load Turnstile (script and frame from
 * Cloudflare). Call it from the frontmatter of a page with a sign-up form;
 * a no-op while the site key is unset.
 */
export function allowTurnstile(csp: AstroGlobal["csp"], siteKey: string | undefined) {
	if (!csp || !siteKey) return;
	csp.insertScriptResource(TURNSTILE_ORIGIN);
	csp.insertDirective(`frame-src ${TURNSTILE_ORIGIN}`);
}

/**
 * allowTurnstile for a page whose form only signed-out visitors check
 * (components/common/HumanCheck.astro): signed in, the page keeps the
 * global policy.
 */
export function allowGuestTurnstile(
	csp: AstroGlobal["csp"],
	siteKey: string | undefined,
	signedIn: boolean,
) {
	if (!signedIn) allowTurnstile(csp, siteKey);
}

/** The parts of Cloudflare's `window.turnstile` used here. */
export interface TurnstileApi {
	render(container: HTMLElement, options: TurnstileOptions): string | null | undefined;
	reset(widgetId: string): void;
}

export interface TurnstileOptions {
	sitekey: string;
	action: TurnstileAction;
	theme: "light" | "dark" | "auto";
	size: "flexible";
	callback(token: string): void;
	"expired-callback"(): void;
	"error-callback"(): void;
}

declare global {
	interface Window {
		/** Set by Cloudflare's script once it has loaded. */
		turnstile?: TurnstileApi;
	}
}

/** A form's human check. */
export interface HumanCheck {
	/** The token to send: "" while the check is off, undefined until it passes. */
	token(): string | undefined;
	/** What to tell the person while token() is undefined. */
	waiting(): string;
	/** Drops the token after an attempt (each is good once) and checks again. */
	reset(): void;
}

/** The check while Turnstile is off: nothing to wait for, no token sent. */
export const noHumanCheck: HumanCheck = { token: () => "", waiting: () => "", reset: () => {} };

const WAITING = "Wait a moment: the check that you are human below has not finished.";
const BROKEN = "The check that you are human could not run. Reload the page to try again.";

/** Renders the widget into container and follows its token. */
export function renderHumanCheck(
	api: TurnstileApi,
	container: HTMLElement,
	options: { siteKey: string; action: TurnstileAction; theme: TurnstileOptions["theme"] },
): HumanCheck {
	let token: string | undefined;
	let broken = false;
	const widgetId = api.render(container, {
		sitekey: options.siteKey,
		action: options.action,
		theme: options.theme,
		size: "flexible",
		callback: (value) => {
			token = value;
			broken = false;
		},
		"expired-callback": () => (token = undefined),
		"error-callback": () => {
			token = undefined;
			broken = true;
		},
	});
	return {
		token: () => token,
		waiting: () => (broken ? BROKEN : WAITING),
		reset: () => {
			token = undefined;
			if (widgetId) api.reset(widgetId);
		},
	};
}

let loading: Promise<TurnstileApi> | undefined;

/** Loads Cloudflare's script once (browser only). */
function loadTurnstile(): Promise<TurnstileApi> {
	loading ??= new Promise<TurnstileApi>((resolve, reject) => {
		const script = document.createElement("script");
		script.src = SCRIPT_URL;
		script.addEventListener("load", () => {
			const api = window.turnstile;
			if (api) resolve(api);
			else reject(new Error("Turnstile loaded without its API"));
		});
		script.addEventListener("error", () => {
			loading = undefined;
			script.remove();
			reject(new Error("Turnstile did not load"));
		});
		document.head.append(script);
	});
	return loading;
}

/**
 * Starts the human check of a form (browser only): container is its
 * `[data-turnstile]` element (data-sitekey, data-action), rendered only while
 * Turnstile is on; without one the check is off.
 */
export async function mountHumanCheck(container: HTMLElement | null): Promise<HumanCheck> {
	const { sitekey, action } = container?.dataset ?? {};
	if (!container || !sitekey || !isAction(action)) {
		return noHumanCheck;
	}
	const theme = document.documentElement.dataset.theme === "dark" ? "dark" : "light";
	try {
		return renderHumanCheck(await loadTurnstile(), container, {
			siteKey: sitekey,
			action,
			theme,
		});
	} catch {
		return {
			token: () => undefined,
			waiting: () => BROKEN,
			reset: () => {},
		};
	}
}
