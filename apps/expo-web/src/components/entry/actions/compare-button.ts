// Drives the Compare button (CompareButton.astro): picks this blueprint,
// opens the comparison with the one picked on another page, or unpicks.
import { compareAction, pickLeaf, pickedLeaf, tabStorage } from "@lib/compare-pick";

const TOOLTIPS = {
	pick: "Pick to compare with another blueprint",
	picked: "Picked: open another blueprint and press Compare. Press again to unpick.",
	open: "Compare with the blueprint you picked",
};

for (const button of document.querySelectorAll<HTMLButtonElement>("[data-compare]")) {
	const id = button.dataset.blueprintId!;
	const render = () => {
		const store = tabStorage();
		const action = compareAction(store && pickedLeaf(store), id);
		button.setAttribute("aria-pressed", String(action.kind === "clear"));
		button.dataset.tooltip =
			action.kind === "clear"
				? TOOLTIPS.picked
				: action.kind === "open"
					? TOOLTIPS.open
					: TOOLTIPS.pick;
	};
	button.addEventListener("click", () => {
		const store = tabStorage();
		if (!store) return;
		const action = compareAction(pickedLeaf(store), id);
		if (action.kind === "open") {
			// The comparison uses the pick up: the next Compare starts fresh.
			pickLeaf(store);
			location.assign(action.href);
			return;
		}
		pickLeaf(store, action.kind === "pick" ? id : undefined);
		render();
	});
	render();
	// Another tab of this one's session never shares sessionStorage, but a
	// back/forward cache restore can show a stale state.
	window.addEventListener("pageshow", render);
}
