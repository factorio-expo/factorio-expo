// "Confirm it's you": changing how an account logs in (link a provider, add
// a passkey, remove either), downloading its data and deleting it need a
// recent login. The
// API refuses a stale session with 403 `reauth_required` (lib/api
// Problem); the account page then asks the person to log in again,
// with a passkey (in the page) or a provider (a round trip through the
// provider and back to /account?reauth=done), and retries the change. The change waits in this
// tab's sessionStorage across that round trip: a page script of this site is
// the only thing that can put one there, so a crafted link can never make
// the page change anything.
import { ApiError, apiErrorCode } from "@lib/api/http-client";

/** A sign-in method change, data download or account deletion waiting for "confirm it's you". */
export type PendingChange =
	| { kind: "remove-identity"; provider: string }
	| { kind: "remove-passkey"; id: string }
	| { kind: "add-passkey" }
	| { kind: "link"; provider: string }
	| { kind: "export" }
	| { kind: "recovery-code" }
	| { kind: "delete-account" };

/** How long a waiting change stays valid: the API's fresh sign-in window. */
export const PENDING_CHANGE_MS = 10 * 60 * 1000;

const KEY = "expo:pending-account-change";

type ChangeStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const isRecord = (value: unknown): value is Record<string, unknown> =>
	typeof value === "object" && value !== null;

/** Whether the API refused a call because the sign-in is not recent enough. */
export function isReauthRequired(err: unknown): boolean {
	return err instanceof ApiError && err.status === 403 && apiErrorCode(err) === "reauth_required";
}

/** This tab's storage, or null where storage is blocked. */
const tabStorage = (): ChangeStorage | null => {
	try {
		return sessionStorage;
	} catch {
		return null;
	}
};

/** Keeps a change for this tab until the person has confirmed it's them. */
export function rememberPendingChange(
	change: PendingChange,
	now = Date.now(),
	storage = tabStorage(),
) {
	try {
		storage?.setItem(KEY, JSON.stringify({ change, at: now }));
	} catch {
		// No storage: after a provider round trip the person repeats the change.
	}
}

/** Returns the waiting change once, if it is still recent, and forgets it. */
export function takePendingChange(
	now = Date.now(),
	storage = tabStorage(),
): PendingChange | undefined {
	try {
		const raw = storage?.getItem(KEY);
		storage?.removeItem(KEY);
		if (!raw) return undefined;
		const stored: unknown = JSON.parse(raw);
		if (!isRecord(stored) || typeof stored.at !== "number") return undefined;
		const age = now - stored.at;
		if (age < 0 || age > PENDING_CHANGE_MS) return undefined;
		return isPendingChange(stored.change) ? stored.change : undefined;
	} catch {
		return undefined;
	}
}

function isPendingChange(value: unknown): value is PendingChange {
	if (!isRecord(value)) return false;
	switch (value.kind) {
		case "remove-identity":
		case "link":
			return typeof value.provider === "string";
		case "remove-passkey":
			return typeof value.id === "string";
		case "add-passkey":
		case "export":
		case "recovery-code":
		case "delete-account":
			return true;
		default:
			return false;
	}
}
