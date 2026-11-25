// The username step of a provider sign-up (SignupForm.astro).
import { ApiError } from "@lib/api";
import { getBrowserApiClient } from "@lib/api/browser";
import { safeNext } from "@lib/auth";
import { runCheckedAction } from "@lib/browser-action";
import { destinationAfterSignIn } from "@lib/recovery-code";
import { setStatus } from "@lib/status";
import { mountHumanCheck, noHumanCheck, type HumanCheck } from "@lib/turnstile";

const form = document.querySelector<HTMLFormElement>("[data-signup]")!;
const intro = form.querySelector<HTMLElement>("[data-signup-intro]")!;
const fields = form.querySelector<HTMLFieldSetElement>("[data-signup-fields]")!;
const status = form.querySelector<HTMLElement>("[data-signup-status]")!;
const input = form.querySelector<HTMLInputElement>('[name="username"]')!;
const api = getBrowserApiClient();
// Started once a sign-up is known to be waiting (the form is usable).
let humanCheck: Promise<HumanCheck> = Promise.resolve(noHumanCheck);

async function load() {
	try {
		const pending = await api.auth.pendingSignup();
		intro.removeAttribute("aria-busy");
		intro.textContent = `You signed in with ${pending.provider_label}. Choose the username shown on your uploads.`;
		input.value = pending.suggestion;
		humanCheck = mountHumanCheck(form.querySelector<HTMLElement>("[data-turnstile]"));
		fields.disabled = false;
		input.focus();
	} catch (err) {
		if (err instanceof ApiError && err.status === 404) {
			form.hidden = true;
			document.querySelector<HTMLElement>("[data-signup-missing]")!.hidden = false;
		} else {
			intro.removeAttribute("aria-busy");
			intro.textContent = "Could not load your sign-in. Reload the page to try again.";
		}
	}
}

form.addEventListener("submit", async (event) => {
	event.preventDefault();
	if (!form.reportValidity()) {
		setStatus(status, "Choose a username of 3-32 letters, digits, _ or -.");
		return;
	}
	const session = await runCheckedAction(
		await humanCheck,
		{
			status,
			controls: [fields],
			busy: "Creating your account...",
			fallback: "Could not create the account.",
			messages: {
				404: "This sign-up expired. Log in again to start over.",
				409: "That username is not available. Choose another one.",
			},
			onUnauthorized: "ignore",
		},
		(token) => api.auth.completeSignup(input.value.trim(), token),
	);
	// A new account's recovery code is shown once, before the page it came for.
	if (session) {
		location.assign(destinationAfterSignIn(session, "signup", safeNext(session.next)));
	}
});

void load();
