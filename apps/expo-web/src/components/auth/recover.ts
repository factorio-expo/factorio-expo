// The recovery form (RecoverForm.astro): signs in with the username and
// recovery code, then shows the new code the recovery issued.
import { getBrowserApiClient } from "@lib/api/browser";
import { runAction } from "@lib/browser-action";
import { destinationAfterSignIn } from "@lib/recovery-code";
import { setStatus } from "@lib/status";

const form = document.querySelector<HTMLFormElement>("[data-recover]")!;
const fields = form.querySelector<HTMLFieldSetElement>("[data-recover-fields]")!;
const status = form.querySelector<HTMLElement>("[data-recover-status]")!;
const username = form.querySelector<HTMLInputElement>('[name="username"]')!;
const code = form.querySelector<HTMLInputElement>('[name="code"]')!;

form.addEventListener("submit", async (event) => {
	event.preventDefault();
	if (!username.value.trim() || !code.value.trim()) {
		setStatus(status, "Enter your username and your recovery code.");
		return;
	}
	const session = await runAction(
		{
			status,
			controls: [fields],
			busy: "Checking your recovery code...",
			fallback: "Could not recover the account.",
			messages: { 401: "That username and recovery code do not match." },
			// A 401 here is a wrong code, not an expired session.
			onUnauthorized: "ignore",
		},
		() => getBrowserApiClient().auth.recover(username.value.trim(), code.value),
	);
	if (session) {
		// The page after the new code is the account page: the recovered session
		// is a fresh sign-in, so a new way to log in can be added at once.
		location.assign(destinationAfterSignIn(session, "recovery", "/account"));
	}
});
