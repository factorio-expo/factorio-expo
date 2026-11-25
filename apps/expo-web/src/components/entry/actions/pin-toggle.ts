// The pin toggle's behavior, shared by the server-rendered PinButton (bound by
// pin-button.ts) and the account list rows built in the browser (entry-list.ts).
import { browserApi, runToggle } from "@lib/browser-action";
import { LIVE_REGIONS, PIN_REGIONS, refreshLiveRegions } from "@lib/live-regions";

export function pinLabel(pinned: boolean) {
	return pinned ? "Unpin from profile" : "Pin to profile";
}

/** Sets a pin button's pressed state, label and tooltip. */
export function renderPin(button: HTMLButtonElement, pinned: boolean) {
	button.setAttribute("aria-pressed", String(pinned));
	button.setAttribute("aria-label", pinLabel(pinned));
	button.dataset.tooltip = pinLabel(pinned);
	button.removeAttribute("aria-description");
}

/** Sets every pin button of the entry on the page to the new state. */
function renderAll(entryId: string, pinned: boolean) {
	for (const button of document.querySelectorAll<HTMLButtonElement>("[data-pin-toggle]")) {
		if (button.dataset.entryId === entryId) renderPin(button, pinned);
	}
}

/** Makes a `[data-pin-toggle][data-entry-id]` button pin or unpin its entry. */
export function bindPinButton(button: HTMLButtonElement) {
	button.addEventListener("click", async () => {
		const entryId = button.dataset.entryId!;
		const pinned = button.getAttribute("aria-pressed") !== "true";
		const users = browserApi().users;
		const saved = await runToggle(button, "Could not change your pins.", () =>
			pinned ? users.pin(entryId) : users.unpin(entryId),
		);
		if (!saved) return;
		renderAll(entryId, pinned);
		if (document.querySelector(`[data-live-region='${LIVE_REGIONS.pins}']`))
			await refreshLiveRegions(PIN_REGIONS);
	});
}
