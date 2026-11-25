// Server-rendered regions that change after the page loaded (a preview that
// finished rendering, an analysis that arrived). Instead of rendering them
// again in the browser, the page fetches itself and swaps each
// `[data-live-region]` for the fresh server markup; the rest of the page (an
// open edit form, scroll position) stays as it is. Scripts that bind to a
// region register with onLiveRegions so they bind the swapped-in markup too.
// Browser-only.

const EVENT = "expo:live-regions";

/**
 * The `data-live-region` names. Templates mark a region with one, scripts
 * refresh only the ones their change affects (a posted comment must not
 * re-render the preview and reset the timescale).
 */
export const LIVE_REGIONS = {
	preview: "preview",
	buildCost: "build-cost",
	analysis: "analysis",
	footprint: "footprint",
	comments: "comments",
	tree: "tree",
	pins: "pins",
	pinsCount: "pins-count",
} as const;
export type LiveRegion = (typeof LIVE_REGIONS)[keyof typeof LIVE_REGIONS];
/** The regions that follow a blueprint's preview and analysis progress. */
export const LEAF_REGIONS: readonly LiveRegion[] = [
	LIVE_REGIONS.preview,
	LIVE_REGIONS.buildCost,
	LIVE_REGIONS.analysis,
	LIVE_REGIONS.footprint,
];
/** The regions of a profile's pinned entries. */
export const PIN_REGIONS: readonly LiveRegion[] = [LIVE_REGIONS.pins, LIVE_REGIONS.pinsCount];

/** Runs bind on the document now and on every region swapped in later. */
export function onLiveRegions(bind: (root: ParentNode) => void) {
	bind(document);
	document.addEventListener(EVENT, (event) => {
		if (!(event instanceof CustomEvent) || !Array.isArray(event.detail)) return;
		for (const root of event.detail) if (root instanceof Element) bind(root);
	});
}

/**
 * Re-renders the page's live regions (only `keys`, when given) from a fresh
 * copy of this URL and returns the fetched document, or undefined when the
 * fetch failed.
 */
export async function refreshLiveRegions(
	keys?: readonly LiveRegion[],
): Promise<Document | undefined> {
	let html: string;
	try {
		const response = await fetch(location.href, {
			credentials: "same-origin",
			headers: { Accept: "text/html" },
		});
		if (!response.ok) return undefined;
		html = await response.text();
	} catch {
		return undefined;
	}
	const fresh = new DOMParser().parseFromString(html, "text/html");
	const swapped: Element[] = [];
	for (const region of document.querySelectorAll<HTMLElement>("[data-live-region]")) {
		const key = region.dataset.liveRegion!;
		if (keys && !keys.some((wanted) => wanted === key)) continue;
		const next = fresh.querySelector(`[data-live-region="${CSS.escape(key)}"]`);
		if (!next) continue;
		const imported = document.importNode(next, true);
		region.replaceWith(imported);
		swapped.push(imported);
	}
	if (swapped.length) document.dispatchEvent(new CustomEvent(EVENT, { detail: swapped }));
	return fresh;
}
