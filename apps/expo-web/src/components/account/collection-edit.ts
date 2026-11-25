// The collection page's edit form: the pencil swaps the details for the form
// (name, description, visibility), Save writes them and reloads.
import { setStatus } from "@lib/status";
import { browserApi, runAction } from "@lib/browser-action";

const root = document.querySelector<HTMLElement>("[data-collection]")!;
const collectionId = root.dataset.collectionId!;
const status = root.querySelector<HTMLElement>("[data-collection-status]")!;
const display = root.querySelector<HTMLElement>("[data-collection-display]")!;
const form = root.querySelector<HTMLFormElement>("[data-collection-form]")!;
const edit = root.querySelector<HTMLButtonElement>("[data-edit-collection]")!;
const field = (name: string) =>
	form.querySelector<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>(
		`[name="${name}"]`,
	)!;

edit.addEventListener("click", () => {
	display.hidden = true;
	form.hidden = false;
	setStatus(status, "");
	field("name").focus();
});

form.querySelector<HTMLButtonElement>("[data-cancel-collection]")!.addEventListener("click", () => {
	form.reset();
	// The markdown editor mirrors its textarea on input events.
	field("description").dispatchEvent(new Event("input", { bubbles: true }));
	form.hidden = true;
	display.hidden = false;
	setStatus(status, "");
	edit.focus();
});

form.addEventListener("submit", async (event) => {
	event.preventDefault();
	const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
	const saved = await runAction(
		{
			status,
			controls: [submit],
			restore: true,
			busy: "Saving...",
			fallback: "Could not save the collection.",
			messages: { 404: "Collection not found." },
			onUnauthorized: "login",
		},
		() =>
			browserApi().collections.update(collectionId, {
				name: field("name").value.trim(),
				description: field("description").value.trim(),
				visibility: field("visibility").value,
			}),
	);
	if (saved) location.reload();
});
