// Account settings: where you're logged in (server-rendered). Logging out
// another session reloads the list; logging out this device, or everywhere,
// ends this page's session too and goes to the home page.
import { getBrowserApiClient } from "@lib/api/browser";
import { runAction } from "@lib/browser-action";

const root = document.querySelector<HTMLElement>("[data-sessions]");

if (root) {
	const status = root.querySelector<HTMLElement>("[data-sessions-status]")!;
	const api = getBrowserApiClient();

	const end = async (call: () => Promise<void>, signsOutHere: boolean) => {
		const done = await runAction(
			{
				status,
				controls: [...root.querySelectorAll<HTMLButtonElement>("button")],
				busy: "Logging out...",
				fallback: "Could not log out.",
				onUnauthorized: "login",
			},
			() => call().then(() => true),
		);
		if (!done) return;
		if (signsOutHere) location.assign("/");
		else location.reload();
	};

	root.addEventListener("click", (event) => {
		if (!(event.target instanceof Element)) return;
		const button = event.target.closest<HTMLButtonElement>("button");
		if (!button) return;
		const session = button.dataset.endSession;
		if (session) {
			void end(() => api.auth.endSession(session), button.hasAttribute("data-current"));
		} else if (button.hasAttribute("data-end-all-sessions")) {
			void end(() => api.auth.endAllSessions(), true);
		}
	});
}
