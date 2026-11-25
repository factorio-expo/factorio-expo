// Makes a textarea a drop target for text files: the file's content replaces
// the field's value, as if pasted. Without it the browser would navigate to a
// dropped file. Dropped text still goes through the textarea's own handling.
// Import only from client-side code.

/** Larger files are not blueprint strings or their JSON (fixtures are a few hundred KB). */
const MAX_FILE_BYTES = 8 * 1024 * 1024;

const hasFiles = (event: DragEvent) => event.dataTransfer?.types.includes("Files") ?? false;

/**
 * Reads a dropped file into `field` and fires `input` and `change` on it.
 * `onError` receives a message when the file is too large or unreadable.
 */
export function bindFileDrop(
	field: HTMLTextAreaElement,
	onError: (message: string) => void = () => {},
) {
	const setDropping = (on: boolean) => field.toggleAttribute("data-dropping", on);
	field.addEventListener("dragover", (event) => {
		if (!hasFiles(event)) return;
		event.preventDefault();
		if (event.dataTransfer) event.dataTransfer.dropEffect = "copy";
		setDropping(true);
	});
	field.addEventListener("dragleave", () => setDropping(false));
	field.addEventListener("drop", async (event) => {
		setDropping(false);
		const file = event.dataTransfer?.files[0];
		if (!file) return;
		event.preventDefault();
		if (file.size > MAX_FILE_BYTES) {
			onError("That file is too large to be a blueprint string.");
			return;
		}
		try {
			field.value = (await file.text()).trim();
		} catch {
			onError("Could not read that file.");
			return;
		}
		field.dispatchEvent(new Event("input", { bubbles: true }));
		field.dispatchEvent(new Event("change", { bubbles: true }));
		field.focus();
	});
}
