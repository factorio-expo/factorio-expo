// Server-side "who is viewing": one /api/v1/me call per request (and only when a
// session cookie exists), resolved by the middleware before the page renders
// and shared by the layout and the page through `locals`. This is the only
// source of the visitor's identity: the page renders it, and the browser
// stores nothing about it.
import { ApiError, type UserResponse } from "@lib/api";
import { getServerApiClient, sessionToken, type ServerRequestContext } from "@lib/api/server";
import { loginPath } from "@lib/safe-next";

export type Viewer =
	| { state: "in"; user: UserResponse }
	| { state: "out" }
	/** The API could not answer: the page renders as logged out. */
	| { state: "unknown" };

type ViewerContext = ServerRequestContext & { locals: App.Locals };
type RedirectContext = ViewerContext & { url: URL; redirect: (path: string) => Response };

async function resolve(context: ServerRequestContext): Promise<Viewer> {
	if (!sessionToken(context.cookies)) return { state: "out" };
	try {
		return { state: "in", user: await getServerApiClient(context).auth.me() };
	} catch (err) {
		if (err instanceof ApiError && err.status === 401) return { state: "out" };
		return { state: "unknown" };
	}
}

/** The viewer of this request; never rejects. */
export function getViewer(context: ViewerContext): Promise<Viewer> {
	context.locals.viewer ??= resolve(context);
	return context.locals.viewer;
}

/** A redirect to /login that comes back to this page afterwards. */
export function redirectToLogin(context: RedirectContext): Response {
	return context.redirect(loginPath(context.url.pathname + context.url.search));
}

/** A redirect to /login (back to this page afterwards) unless someone is signed in. */
export async function redirectToLoginUnlessSignedIn(
	context: RedirectContext,
): Promise<Response | undefined> {
	if ((await getViewer(context)).state === "in") return undefined;
	return redirectToLogin(context);
}

/** A redirect to `next` when someone is already signed in (login and signup pages). */
export async function redirectIfSignedIn(
	context: ViewerContext & { redirect: (path: string) => Response },
	next: string,
): Promise<Response | undefined> {
	return (await getViewer(context)).state === "in" ? context.redirect(next) : undefined;
}
