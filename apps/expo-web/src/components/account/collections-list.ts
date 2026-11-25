// Collections page (/account/collections): the cards are server-rendered
// (CollectionGrid); this creates a collection from the "New collection"
// dialog (then reloads, so it shows as a card) and deletes one in place.
import { bindDialog } from "@lib/dialog";
import { setStatus } from "@lib/status";
import { browserApi, runAction } from "@lib/browser-action";

const status = document.querySelector<HTMLElement>("[data-collections-status]")!;
const rows = document.querySelector<HTMLElement>("[data-collection-rows]")!;
const form = document.querySelector<HTMLFormElement>("[data-collection-form]")!;
const formStatus = form.querySelector<HTMLElement>("[data-collection-form-status]")!;
const dialog = form.closest("dialog")!;
const nameInput = form.querySelector<HTMLInputElement>('[name="name"]')!;

bindDialog({
	dialog,
	opener: document.querySelector<HTMLElement>("[data-new-collection]")!,
	onOpen: () => {
		form.reset();
		setStatus(formStatus, "");
		nameInput.focus();
	},
});
for (const cancel of dialog.querySelectorAll<HTMLElement>("form [data-dialog-close]")) {
	cancel.addEventListener("click", () => dialog.close());
}

for (const remove of rows.querySelectorAll<HTMLButtonElement>("[data-delete-collection]")) {
	const id = remove.dataset.deleteCollection!;
	const name = remove.dataset.name ?? "";
	remove.addEventListener("click", async () => {
		if (!confirm(`Delete "${name}"? The entries themselves are kept.`)) return;
		const deleted = await runAction(
			{
				status,
				controls: [remove],
				fallback: "Could not delete the collection.",
				onUnauthorized: "login",
			},
			() =>
				browserApi()
					.collections.delete(id)
					.then(() => true),
		);
		if (!deleted) return;
		remove.closest("li")?.remove();
		if (!rows.children.length) setStatus(status, "You have no collections yet.");
	});
}

form.addEventListener("submit", async (event) => {
	event.preventDefault();
	const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
	const visibility = form.querySelector<HTMLSelectElement>('[name="visibility"]')!;
	const collection = await runAction(
		{
			status: formStatus,
			controls: [submit],
			restore: true,
			fallback: "Could not create the collection.",
			onUnauthorized: "login",
		},
		() =>
			browserApi().collections.create({
				name: nameInput.value.trim(),
				visibility: visibility.value,
			}),
	);
	if (collection) location.reload();
});

// Rendered disabled until this handler exists (see account/collections/index.astro).
form.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled = false;
