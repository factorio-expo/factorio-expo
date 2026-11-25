// Open/close behaviour shared by the modal pickers: showModal, close button,
// click on the backdrop, and focus returning to the opener.

export interface DialogParts {
	dialog: HTMLDialogElement;
	opener: HTMLElement;
	/** Called after the dialog is shown (e.g. to load its content). */
	onOpen?: () => void;
}

export function bindDialog({ dialog, opener, onOpen }: DialogParts) {
	opener.addEventListener("click", () => {
		dialog.showModal();
		onOpen?.();
	});
	dialog.addEventListener("close", () => opener.focus());
	dialog
		.querySelector<HTMLElement>("[data-dialog-close]")
		?.addEventListener("click", () => dialog.close());
	dialog.addEventListener("click", (event) => {
		if (event.target === dialog) dialog.close();
	});
}
