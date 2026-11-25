// The ?error= codes the API's provider sign-in sends the browser back with
// (apps/expo-api internal/httpapi/auth_oauth.go), as text for the login and
// account pages.
const messages: Record<string, string> = {
	cancelled: "Sign-in was cancelled.",
	failed: "Sign-in did not complete. Try again.",
	expired: "The sign-in took too long or was started in another browser. Try again.",
	"identity-taken": "That account is already linked to another Factorio Expo account.",
	"provider-linked":
		"An account of this provider is already linked. Remove it first to link another one.",
	"signups-paused": "New sign-ups are paused right now. Existing accounts can still log in.",
	"identity-held":
		"That account belonged to a deleted Factorio Expo account that is banned. It can be used again once the ban ends.",
	// Account page only: linking needs a recent login (lib/fresh-sign-in.ts).
	reauth_required: "Confirm it's you before linking another account.",
	"reauth-other-account":
		"That login belongs to another account. Confirm with one of this account's sign-in methods.",
};

/** The text for an ?error= code, or undefined when there is none. */
export function signInErrorText(code: string | null | undefined): string | undefined {
	if (!code) return undefined;
	return messages[code] ?? messages.failed;
}
