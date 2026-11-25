// Drives every PinButton. The server rendered each one's state; a change on
// the profile page re-renders its pinned entries (the "pins" live region),
// whose cards carry pin buttons of their own, bound again here.
import { onLiveRegions } from "@lib/live-regions";
import { bindPinButton } from "./pin-toggle.ts";

onLiveRegions((scope) => {
	for (const button of scope.querySelectorAll<HTMLButtonElement>("[data-pin-toggle]"))
		bindPinButton(button);
});
