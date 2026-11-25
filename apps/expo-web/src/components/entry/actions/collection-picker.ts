// Drives CollectionPicker: lists the viewer's collections with membership
// checkboxes for one entry. Ticking and "New collection" (a named row, ticked)
// only change the form; Save writes the differences (adds, removes, creates
// holding the entry) and closes the dialog.
import { bindDialog } from "@lib/dialog";
import { setStatus } from "@lib/status";
import type { Collection } from "@lib/api";
import { browserApi, runAction } from "@lib/browser-action";
import { formatRichTextSummary } from "@factorio-expo/ui/game/rich-text.ts";

const MAX_NAME = 100;

/** A checkbox row: an existing collection (with its saved state) or a new one. */
interface Row {
	checkbox: HTMLInputElement;
	collection?: Collection;
	saved: boolean;
	name?: HTMLInputElement;
}

for (const picker of document.querySelectorAll<HTMLElement>("[data-collection-picker]")) {
	const entryId = picker.dataset.entryId!;
	const open = picker.querySelector<HTMLButtonElement>("[data-open-collections]")!;
	const dialog = picker.querySelector<HTMLDialogElement>("dialog")!;
	const form = picker.querySelector<HTMLFormElement>("[data-collection-form]")!;
	const list = picker.querySelector<HTMLUListElement>("[data-collection-list]")!;
	const status = picker.querySelector<HTMLElement>("[data-collection-status]")!;
	const save = picker.querySelector<HTMLButtonElement>("[data-collection-save]")!;
	const create = picker.querySelector<HTMLButtonElement>("[data-collection-new]")!;
	let rows: Row[] = [];

	function addRow(collection: Collection, member: boolean) {
		const item = document.createElement("li");
		const label = document.createElement("label");
		const checkbox = document.createElement("input");
		checkbox.type = "checkbox";
		checkbox.checked = member;
		const name = document.createElement("span");
		name.innerHTML = formatRichTextSummary(collection.name);
		const visibility = document.createElement("small");
		visibility.textContent = ` · ${collection.visibility}`;
		label.append(checkbox, name, visibility);
		item.append(label);
		list.append(item);
		rows.push({ checkbox, collection, saved: member });
	}

	function addNewRow() {
		const item = document.createElement("li");
		const label = document.createElement("label");
		const checkbox = document.createElement("input");
		checkbox.type = "checkbox";
		checkbox.checked = true;
		checkbox.setAttribute("aria-label", "Add to the new collection");
		const name = document.createElement("input");
		name.type = "text";
		name.maxLength = MAX_NAME;
		name.placeholder = "New collection name";
		name.setAttribute("aria-label", "New collection name");
		label.append(checkbox, name);
		item.append(label);
		list.append(item);
		rows.push({ checkbox, saved: false, name });
		setStatus(status, "");
		name.focus();
	}

	let loadToken = 0;
	async function load() {
		// Reopening while a load is in flight must not append a second copy of the rows.
		const token = ++loadToken;
		list.replaceChildren();
		rows = [];
		save.disabled = true;
		const loaded = await runAction(
			{
				status,
				busy: "Loading collections...",
				fallback: "Could not load your collections.",
				onUnauthorized: "login",
			},
			() => browserApi().collections.listForEntry(entryId),
		);
		if (!loaded || token !== loadToken) return;
		for (const collection of loaded.collections) addRow(collection, collection.contains);
		save.disabled = false;
		setStatus(
			status,
			loaded.collections.length
				? ""
				: "You have no collections yet. Create one with New collection.",
		);
	}

	bindDialog({ dialog, opener: open, onOpen: () => void load() });
	create.addEventListener("click", addNewRow);

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const fresh = rows.filter((row) => row.name && row.checkbox.checked);
		if (fresh.some((row) => !row.name!.value.trim())) {
			setStatus(status, "Name the new collection, or untick it.");
			fresh.find((row) => !row.name!.value.trim())?.name!.focus();
			return;
		}
		const changed = rows.filter((row) => row.collection && row.checkbox.checked !== row.saved);
		const api = browserApi().collections;
		const saved = await runAction(
			{
				status,
				controls: [save, create],
				restore: true,
				busy: "Saving...",
				fallback: "Could not save your collections.",
				onUnauthorized: "login",
			},
			async () => {
				await Promise.all([
					...changed.map(async (row) => {
						const id = row.collection!.id;
						if (row.checkbox.checked) await api.addEntry(id, entryId);
						else await api.removeEntry(id, entryId);
						row.saved = row.checkbox.checked;
					}),
					...fresh.map(async (row) => {
						const created = await api.create({ name: row.name!.value.trim() });
						await api.addEntry(created.id, entryId);
						// Saved: a retry after a later failure must not create it twice.
						row.name = undefined;
						row.collection = created;
						row.saved = true;
					}),
				]);
				return true;
			},
		);
		if (saved) dialog.close();
	});
}
