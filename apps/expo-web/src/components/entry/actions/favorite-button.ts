// Drives every FavoriteButton. The server rendered each one's state and reveals
// the toggles to signed-in viewers (data-auth), so nothing is looked up here.
import { runToggle } from "@lib/browser-action";
import { setFavorite } from "@lib/favorites";
import { onLiveRegions } from "@lib/live-regions";

function render(root: HTMLElement, favorited: boolean) {
	const button = root.querySelector<HTMLButtonElement>("[data-favorite-toggle]")!;
	const text = root.querySelector<HTMLElement>("[data-favorite-text]");
	if (text && root.dataset.count !== undefined) text.textContent = root.dataset.count;
	button.setAttribute("aria-pressed", String(favorited));
	button.setAttribute("aria-label", favorited ? "Remove from favorites" : "Add to favorites");
	button.dataset.tooltip = button.getAttribute("aria-label")!;
}

function adjustCount(root: HTMLElement, delta: number) {
	if (root.dataset.count === undefined) return;
	root.dataset.count = String(Math.max(0, Number(root.dataset.count) + delta));
}

function bind(root: HTMLElement) {
	const button = root.querySelector<HTMLButtonElement>("[data-favorite-toggle]")!;
	button.addEventListener("click", async () => {
		const favorited = button.getAttribute("aria-pressed") !== "true";
		const saved = await runToggle(button, "Could not update your favorites.", () =>
			setFavorite(root.dataset.entryId!, favorited),
		);
		if (!saved) return;
		adjustCount(root, favorited ? 1 : -1);
		render(root, favorited);
	});
}

// Cards in a live region (the profile's pinned entries) are swapped in later.
onLiveRegions((scope) => {
	for (const root of scope.querySelectorAll<HTMLElement>("[data-favorite]")) bind(root);
});
