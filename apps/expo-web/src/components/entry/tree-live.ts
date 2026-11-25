// Live book tree: while a card's preview is pending, follow the entry's
// progress (lib/entry-progress.ts watchPreviews) and swap in the server-rendered
// tree (lib/live-regions.ts) whenever the set of pending previews changes, so
// thumbnails appear as they are rendered. Open sub-books and scroll position
// survive: only the region's markup is replaced, and the open state of its
// <details> is carried over.
import { entryEventsUrl } from "@lib/api/browser";
import { watchPreviews } from "@lib/entry-progress";
import { LIVE_REGIONS, refreshLiveRegions } from "@lib/live-regions";

const REGION = `[data-live-region="${LIVE_REGIONS.tree}"]`;
const PENDING = "[data-preview-pending][data-blueprint-id]";

/** The pending previews the page shows, spelled as entry-progress pendingPreviews. */
const shown = () =>
	[...document.querySelectorAll<HTMLElement>(PENDING)]
		.map((card) => card.dataset.blueprintId!)
		.toSorted()
		.join(",");

/** The ids of the <details> that are open, to reopen after a swap. */
const openIds = (root: ParentNode) =>
	[...root.querySelectorAll<HTMLDetailsElement>("details[open]")].map((d) => detailsKey(d));

/** A <details>' place in the tree: its index among all of the tree's <details>. */
function detailsKey(details: HTMLDetailsElement) {
	const all = details.closest(REGION)!.querySelectorAll("details");
	return [...all].indexOf(details);
}

function start() {
	const region = document.querySelector<HTMLElement>(REGION);
	if (!region?.dataset.entryId || !shown()) return;
	watchPreviews(
		{ eventsUrl: entryEventsUrl(region.dataset.entryId) },
		{
			shown,
			refresh: async () => {
				const current = document.querySelector<HTMLElement>(REGION);
				const open = current ? openIds(current) : [];
				await refreshLiveRegions([LIVE_REGIONS.tree]);
				const next = document.querySelector<HTMLElement>(REGION);
				if (!next) return;
				const all = next.querySelectorAll("details");
				for (const index of open) {
					const details = all[index];
					if (details) details.open = true;
				}
				for (const [index, details] of all.entries()) {
					if (!open.includes(index)) details.open = false;
				}
			},
			gone: () => {},
		},
	);
}

start();
