// Live leaf view: while a leaf's preview or analysis is pending, follow it
// (lib/entry-progress.ts watchLeaf) and swap in the server-rendered preview and
// analysis regions once its state differs from what the page shows
// (lib/live-regions.ts). Work queued again from the page (the preview modal's
// re-render) dispatches LEAF_REQUEUED and is followed the same way.
import { entryEventsUrl } from "@lib/api/browser";
import { watchLeaf, type ProgressWatch } from "@lib/entry-progress";
import { LEAF_REGIONS, refreshLiveRegions } from "@lib/live-regions";

/** What the page shows, as `preview:analysis` statuses (entry-progress leafState). */
const shown = (root: HTMLElement) => `${root.dataset.previewStatus}:${root.dataset.analysisStatus}`;

/** Bubbles from inside a `[data-live-leaf]` whose phase was queued again;
 * `detail` is the phase ("preview" or "analysis"). */
export const LEAF_REQUEUED = "expo:leaf-requeued";

const watches = new WeakMap<HTMLElement, ProgressWatch>();

function watch(root: HTMLElement) {
	const blueprintId = root.dataset.blueprintId!;
	if (!shown(root).includes("pending")) return;
	watches.get(root)?.close();
	const progress = watchLeaf({ eventsUrl: entryEventsUrl(root.dataset.entryId!) }, blueprintId, {
		shown: () => shown(root),
		refresh: async () => {
			const fresh = await refreshLiveRegions(LEAF_REGIONS);
			const next = fresh?.querySelector<HTMLElement>(
				`[data-live-leaf][data-blueprint-id="${CSS.escape(blueprintId)}"]`,
			);
			if (next) {
				root.dataset.previewStatus = next.dataset.previewStatus;
				root.dataset.analysisStatus = next.dataset.analysisStatus;
			}
		},
		gone: () => {
			// A quick preview's panel says why it is gone.
			if (document.querySelector("[data-quick-preview]")) return;
			const notice = document.createElement("p");
			notice.className = "live-gone";
			notice.setAttribute("role", "alert");
			notice.textContent = "This entry has been deleted or is no longer visible.";
			root.prepend(notice);
		},
	});
	watches.set(root, progress);
}

document.addEventListener(LEAF_REQUEUED, (event) => {
	if (!(event instanceof CustomEvent) || !(event.target instanceof Element)) return;
	const root = event.target.closest<HTMLElement>("[data-live-leaf]");
	if (!root) return;
	if (event.detail === "analysis") root.dataset.analysisStatus = "pending";
	else root.dataset.previewStatus = "pending";
	watch(root);
});

for (const root of document.querySelectorAll<HTMLElement>("[data-live-leaf]")) watch(root);
