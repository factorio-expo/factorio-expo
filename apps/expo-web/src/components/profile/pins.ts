// The owner's "Arrange" button on their pinned entries: opens the dialog and
// tells PinArranger.svelte to load the pins (PINS_OPEN_EVENT).
import { bindDialog } from "@lib/dialog";

/** Sent on the document when the arrange dialog opens. */
export const PINS_OPEN_EVENT = "expo:pins-open";

export function bindPinsDialog() {
	const dialog = document.querySelector<HTMLDialogElement>("[data-pins-dialog-root] dialog");
	const opener = document.querySelector<HTMLElement>("[data-pins-arrange]");
	if (!dialog || !opener) return;
	bindDialog({
		dialog,
		opener,
		onOpen: () => document.dispatchEvent(new CustomEvent(PINS_OPEN_EVENT)),
	});
}
