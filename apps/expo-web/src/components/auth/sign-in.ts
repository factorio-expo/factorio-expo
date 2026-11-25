// Passkey sign-in and sign-up for SignIn.astro. Provider sign-ins are plain
// links to the API and need no script.
import { getBrowserApiClient } from "@lib/api/browser";
import { safeNext } from "@lib/auth";
import { runAction, runCheckedAction } from "@lib/browser-action";
import { passkeysSupported, signInWithPasskey, signUpWithPasskey } from "@lib/passkeys";
import { destinationAfterSignIn } from "@lib/recovery-code";
import { setStatus } from "@lib/status";
import { mountHumanCheck, type HumanCheck } from "@lib/turnstile";

const root = document.querySelector<HTMLElement>("[data-sign-in]")!;
const next = safeNext(root.dataset.next);
const status = root.querySelector<HTMLElement>("[data-sign-in-status]")!;
const loginButton = root.querySelector<HTMLButtonElement>("[data-passkey-login]")!;
const signupForm = root.querySelector<HTMLFormElement>("[data-passkey-signup]")!;
const signupFields = signupForm.querySelector<HTMLFieldSetElement>("fieldset")!;
const signupButton = signupForm.querySelector<HTMLButtonElement>('button[type="submit"]')!;
const usernameInput = signupForm.querySelector<HTMLInputElement>('[name="username"]')!;
const signupMode = root.querySelector<HTMLInputElement>('[name="passkey-mode"][value="signup"]')!;

// The sign-up human check loads only once someone picks "I'm new here", so
// signing in never contacts Cloudflare.
let humanCheck: Promise<HumanCheck> | undefined;
const startHumanCheck = () =>
	(humanCheck ??= mountHumanCheck(signupForm.querySelector<HTMLElement>("[data-turnstile]")));

if (!passkeysSupported()) {
	root.querySelector<HTMLElement>("[data-passkeys]")!.hidden = true;
	root.querySelector<HTMLElement>("[data-no-passkeys]")!.hidden = false;
} else {
	loginButton.addEventListener("click", async () => {
		const session = await runAction(
			{
				status,
				controls: [loginButton, signupFields],
				busy: "Waiting for your passkey...",
				fallback: "Passkey sign-in was cancelled or did not work.",
				messages: {
					401: 'This passkey is not registered here. Choose "I\'m new here" to create an account, or use another sign-in.',
				},
				// A 401 here is an unknown passkey, not an expired session.
				onUnauthorized: "ignore",
			},
			() => signInWithPasskey(getBrowserApiClient()),
		);
		// The API set the session cookie; the next page renders the visitor signed in.
		if (session) location.assign(next);
	});

	signupForm.addEventListener("submit", async (event) => {
		event.preventDefault();
		if (!signupForm.reportValidity()) {
			setStatus(status, "Choose a username of 3-32 letters, digits, _ or -.");
			return;
		}
		const session = await runCheckedAction(
			await startHumanCheck(),
			{
				status,
				controls: [loginButton, signupFields],
				busy: "Waiting for your passkey...",
				fallback: "The passkey was cancelled or could not be created.",
				messages: { 409: "That username is not available. Choose another one." },
				onUnauthorized: "ignore",
			},
			(token) => signUpWithPasskey(getBrowserApiClient(), usernameInput.value.trim(), token),
		);
		// A new account's recovery code is shown once, before the page it came for.
		if (session) location.assign(destinationAfterSignIn(session, "signup", next));
	});

	signupMode.addEventListener("change", () => void startHumanCheck());
	// The browser may restore "I'm new here" on back/forward navigation.
	if (signupMode.checked) void startHumanCheck();

	// Rendered disabled until these handlers exist (see SignIn.astro).
	loginButton.disabled = false;
	signupButton.disabled = false;
}
