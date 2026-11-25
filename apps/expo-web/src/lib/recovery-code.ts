// The recovery code a sign-up or a recovery just made. The API puts it in the
// response of that one request, and only a hash of it is stored, so the page
// that made the request hands it to /recovery-code through this tab's
// sessionStorage (the same way lib/fresh-sign-in.ts keeps a waiting change).
// It stays there until the person continues, so a reload of that page shows it
// again; closing the tab drops it. Never put it in a URL.

/**
 * Why the code is shown: a new account, a recovery that used the old code up,
 * or one the person asked for on the account page (replacing the old one).
 */
export type RecoveryCodeReason = "signup" | "recovery" | "regenerate";

export interface PendingRecoveryCode {
	code: string;
	username: string;
	reason: RecoveryCodeReason;
	/** Where "Continue" goes: a path on the site. */
	next: string;
}

/** The page that shows the code. */
export const RECOVERY_CODE_PATH = "/recovery-code";

const KEY = "expo:recovery-code";

type CodeStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

/** This tab's storage, or null where storage is blocked. */
const tabStorage = (): CodeStorage | null => {
	try {
		return sessionStorage;
	} catch {
		return null;
	}
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
	typeof value === "object" && value !== null;

/** Keeps the code for /recovery-code. False when this browser cannot store it. */
export function rememberRecoveryCode(
	pending: PendingRecoveryCode,
	storage = tabStorage(),
): boolean {
	try {
		if (!storage) return false;
		storage.setItem(KEY, JSON.stringify(pending));
		return true;
	} catch {
		return false;
	}
}

/** The waiting code, if any; it stays until `forgetRecoveryCode`. */
export function readRecoveryCode(storage = tabStorage()): PendingRecoveryCode | undefined {
	try {
		const raw = storage?.getItem(KEY);
		if (!raw) return undefined;
		const stored: unknown = JSON.parse(raw);
		if (
			!isRecord(stored) ||
			typeof stored.code !== "string" ||
			typeof stored.username !== "string" ||
			typeof stored.next !== "string" ||
			(stored.reason !== "signup" &&
				stored.reason !== "recovery" &&
				stored.reason !== "regenerate")
		) {
			return undefined;
		}
		return {
			code: stored.code,
			username: stored.username,
			reason: stored.reason,
			next: stored.next,
		};
	} catch {
		return undefined;
	}
}

/** Drops the code once the person has continued. */
export function forgetRecoveryCode(storage = tabStorage()) {
	try {
		storage?.removeItem(KEY);
	} catch {
		// Nothing was stored.
	}
}

/** What a sign-up or recovery response needs to show the code. */
export interface SignInWithCode {
	username: string;
	recovery_code?: string;
}

/**
 * Where the browser goes after a sign-up or a recovery: the page showing the
 * new recovery code when the response carries one (and this browser can hold
 * it), otherwise straight to `next`.
 */
export function destinationAfterSignIn(
	session: SignInWithCode,
	reason: RecoveryCodeReason,
	next: string,
	storage = tabStorage(),
): string {
	if (!session.recovery_code) return next;
	const kept = rememberRecoveryCode(
		{ code: session.recovery_code, username: session.username, reason, next },
		storage,
	);
	return kept ? RECOVERY_CODE_PATH : next;
}

/** The text file the person can save: the code and what it is for. */
export function recoveryCodeFile(pending: PendingRecoveryCode, origin: string): string {
	return [
		"Factorio Expo recovery code",
		"",
		`Username: ${pending.username}`,
		`Recovery code: ${pending.code}`,
		"",
		`If you lose every way you log in, go to ${origin}/recover and enter the`,
		"username and this code. It works once; you get a new code when you use it.",
		"Anyone who has this code and your username can sign in as you, so keep it private.",
		"",
	].join("\n");
}

/** The file's name. */
export const recoveryCodeFileName = (username: string) => `factorio-expo-recovery-${username}.txt`;
