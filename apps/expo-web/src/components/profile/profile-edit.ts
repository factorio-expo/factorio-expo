// The owner's profile form (ProfileEditForm): the pencil beside the name swaps
// the name block for the form, as on an entry page. Saves the status line and
// every social name in one call, then reloads the page so the header shows
// what the API stored (it trims names and drops a leading @).
import type { SocialPlatform } from "@lib/api";
import { browserApi, runAction } from "@lib/browser-action";
import { isSocialPlatform } from "@lib/social-links";
import { setStatus } from "@lib/status";

const root = document.querySelector<HTMLElement>("[data-profile]");
const form = root?.querySelector<HTMLFormElement>("[data-profile-form]");
const open = root?.querySelector<HTMLButtonElement>("[data-profile-edit-open]");
if (root && form && open) {
	const display = root.querySelector<HTMLElement>("[data-profile-display]")!;
	const statusLine = form.querySelector<HTMLElement>("[data-profile-status-line]")!;
	const status = form.querySelector<HTMLInputElement>('[name="status"]')!;

	open.addEventListener("click", () => {
		display.hidden = true;
		form.hidden = false;
		setStatus(statusLine, "");
		status.focus();
	});
	form.querySelector<HTMLButtonElement>("[data-profile-cancel]")!.addEventListener(
		"click",
		() => {
			form.reset();
			form.hidden = true;
			display.hidden = false;
			setStatus(statusLine, "");
			open.focus();
		},
	);
	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const links: { platform: SocialPlatform; handle: string }[] = [];
		for (const input of form.querySelectorAll<HTMLInputElement>("[data-link-platform]")) {
			const platform = input.dataset.linkPlatform;
			const handle = input.value.trim();
			if (isSocialPlatform(platform) && handle) links.push({ platform, handle });
		}
		const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
		const saved = await runAction(
			{
				status: statusLine,
				controls: [submit],
				busy: "Saving...",
				fallback: "Could not save your profile.",
				onUnauthorized: "login",
			},
			() => browserApi().users.updateProfile({ status: status.value, links }),
		);
		if (saved) location.reload();
	});
}
