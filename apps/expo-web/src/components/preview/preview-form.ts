// Quick preview submission: creates the preview, hands an anonymous creator's
// edit token to the entry page (EditLinkBanner) and opens the preview on its
// clean link.
import { browserApi, runCheckedAction } from "@lib/browser-action";
import { rememberEditLink } from "@lib/edit-link";
import { bindFileDrop } from "@lib/file-drop";
import { setStatus } from "@lib/status";
import { mountHumanCheck } from "@lib/turnstile";

for (const form of document.querySelectorAll<HTMLFormElement>("[data-preview-form]")) {
	const fields = form.querySelector<HTMLFieldSetElement>("[data-preview-fields]")!;
	const status = form.querySelector<HTMLParagraphElement>("[data-preview-status]")!;
	const blueprint = form.querySelector<HTMLTextAreaElement>('[name="blueprint"]')!;
	// A text file dropped on the box is read as if pasted.
	bindFileDrop(blueprint, (message) => setStatus(status, message));
	// Signed-out visitors pass Turnstile (rendered only for them).
	const humanCheck = mountHumanCheck(form.querySelector<HTMLElement>("[data-turnstile]"));

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		if (!blueprint.value.trim()) {
			blueprint.setCustomValidity("Paste a blueprint exchange string.");
			blueprint.reportValidity();
			blueprint.setCustomValidity("");
			return;
		}
		const entry = await runCheckedAction(
			await humanCheck,
			{
				status,
				controls: [fields],
				busy: "Creating preview...",
				fallback: "Could not create the preview.",
				onUnauthorized: "clear",
			},
			(token) => browserApi().entries.createPreview(blueprint.value, token || undefined),
		);
		if (!entry) return;
		if (entry.edit_token) rememberEditLink(entry.id, entry.edit_token);
		location.assign(`/e/${encodeURIComponent(entry.id)}`);
	});
	form.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled = false;
}
