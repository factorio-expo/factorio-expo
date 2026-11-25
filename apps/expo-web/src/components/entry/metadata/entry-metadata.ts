// Inline editing of an entry's title, description, tags and (owned entries)
// visibility for EntryMetadata: one pencil opens one form, one Save writes all.
import { setStatus } from "@lib/status";
import { browserApi, runAction } from "@lib/browser-action";
import { submittedEntryDetails } from "@components/entry-details/entry-details-form";
import { aliasPath } from "@lib/entry-source";

for (const container of document.querySelectorAll<HTMLElement>("[data-entry-metadata]")) {
	const entryId = container.dataset.entryId!;
	const status = container.querySelector<HTMLElement>("[data-metadata-status]")!;
	const display = container.querySelector<HTMLElement>("[data-metadata-display]")!;
	const form = container.querySelector<HTMLFormElement>("[data-metadata-form]");
	const edit = container.querySelector<HTMLButtonElement>("[data-edit-details]");
	// Both are rendered only for editors (the server decided).
	if (!form || !edit) continue;
	const title = form.querySelector<HTMLInputElement>('[name="title"]')!;
	const description = form.querySelector<HTMLTextAreaElement>('[name="description"]')!;

	edit.addEventListener("click", () => {
		display.hidden = true;
		form.hidden = false;
		setStatus(status, "");
		title.focus();
	});
	form.querySelector<HTMLButtonElement>("[data-cancel-details]")!.addEventListener(
		"click",
		() => {
			form.reset();
			// The markdown editor mirrors its textarea on input events.
			description.dispatchEvent(new Event("input", { bubbles: true }));
			form.hidden = true;
			display.hidden = false;
			setStatus(status, "");
			edit.focus();
		},
	);
	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const details = submittedEntryDetails(form);
		if (!details) return;
		const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
		const saved = await runAction(
			{
				status,
				controls: [submit],
				busy: "Saving...",
				fallback: "Could not save entry details.",
				onUnauthorized: "clear",
			},
			() =>
				// The edit form has no expiry select, so details carry none.
				browserApi().entries.update(entryId, details),
		);
		if (!saved) return;
		// On an alias URL the old alias may be gone: stay on the new one, or leave for /e/{id}.
		if (location.pathname.startsWith("/@")) {
			location.assign((aliasPath(saved) ?? `/e/${entryId}`) + location.search);
		} else {
			location.reload();
		}
	});
}
