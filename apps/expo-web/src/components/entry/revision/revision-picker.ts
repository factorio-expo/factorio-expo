// Loads the revision list into the modal when RevisionPicker's button is used.
import { bindDialog } from "@lib/dialog";
import { localIsoDateTime } from "@factorio-expo/ui/utils/dates.ts";
import { setStatus } from "@lib/status";
import { browserApi, runAction } from "@lib/browser-action";
import { editorLinks } from "@lib/entry-links";

for (const picker of document.querySelectorAll<HTMLElement>("[data-revision-picker]")) {
	const dialog = picker.querySelector<HTMLDialogElement>("dialog")!;
	const list = picker.querySelector<HTMLUListElement>("[data-revision-list]")!;
	const status = picker.querySelector<HTMLElement>("[data-revision-status]")!;
	const open = picker.querySelector<HTMLButtonElement>("[data-open-revisions]")!;
	bindDialog({ dialog, opener: open, onOpen: () => void load() });

	async function load() {
		list.replaceChildren();
		const loaded = await runAction(
			{
				status,
				busy: "Loading revisions...",
				fallback: "Could not load revision history. Close and reopen to retry.",
				onUnauthorized: "ignore",
			},
			() => browserApi().entries.listRevisions(picker.dataset.entryId!),
		);
		if (!loaded) return;
		const { revisions } = loaded;
		for (const revision of revisions) {
			const item = document.createElement("li");
			const link = document.createElement("a");
			link.href = editorLinks(location.href).entry(
				picker.dataset.entryId!,
				revision.id === picker.dataset.currentId ? undefined : revision.id,
			);
			link.textContent = `Revision ${revision.version_number}${revision.id === picker.dataset.currentId ? " (current)" : ""}`;
			if (revision.id === picker.dataset.selectedId)
				link.setAttribute("aria-current", "true");
			const date = document.createElement("time");
			date.dateTime = revision.created_at;
			date.textContent = localIsoDateTime(revision.created_at);
			item.append(link, date);
			if (revision.note) {
				const note = document.createElement("p");
				note.className = "revision-note";
				note.textContent = revision.note;
				item.append(note);
			}
			list.append(item);
		}
		setStatus(
			status,
			revisions.length
				? "Select a revision to view its blueprint content."
				: "No revisions found.",
		);
	}
}
