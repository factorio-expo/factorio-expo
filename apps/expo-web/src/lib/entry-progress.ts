// Live entry progress: the `GET /api/v1/entries/{id}/events` stream (docs/api.md
// "Entry progress events"). Live updates are a convenience: when the stream
// fails for good (an error status such as 429), the page simply stops
// updating and a reload shows the current state. Pure apart from the injected
// EventSource, so it is unit-tested.
import type { EntryProgress, LeafProgress, PhaseChange, PhaseState } from "@lib/api/types";

export interface ProgressHandlers {
	/** The whole current state, after every snapshot or phase event. */
	onState(state: EntryProgress): void;
	/** The entry was deleted or is no longer visible. */
	onGone?(): void;
}

export interface WatchOptions {
	eventsUrl: string;
	EventSource?: typeof EventSource;
}

export interface ProgressWatch {
	close(): void;
}

/** Applies one phase event to the state; unknown leaves are ignored. */
export function applyChange(state: EntryProgress, change: PhaseChange): EntryProgress {
	const leaves = state.leaves.map((leaf) =>
		leaf.blueprint_id === change.blueprint_id
			? { ...leaf, [change.phase]: change.state }
			: leaf,
	);
	const done = leaves.every(
		(leaf) => leaf.preview.status !== "pending" && leaf.analysis.status !== "pending",
	);
	return { ...state, leaves, done };
}

/**
 * Watches an entry until every leaf is settled, the entry is gone, the
 * browser gives up on the stream or close() is called. A dropped connection
 * is retried by the EventSource itself.
 */
export function watchEntryProgress(
	options: WatchOptions,
	handlers: ProgressHandlers,
): ProgressWatch {
	const { eventsUrl, EventSource: Source = globalThis.EventSource } = options;
	let closed = false;
	let state: EntryProgress | undefined;
	const stream = new Source(eventsUrl, { withCredentials: true });

	const stop = () => {
		closed = true;
		stream.close();
	};
	const update = (next: EntryProgress) => {
		state = next;
		handlers.onState(next);
		if (next.done) stop();
	};
	const on = (name: string, handle: (data: unknown) => void) =>
		stream.addEventListener(name, (event) => {
			if (closed || !(event instanceof MessageEvent) || typeof event.data !== "string")
				return;
			handle(JSON.parse(event.data));
		});
	// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- the event shapes are the API's contract (types.ts mirrors Go)
	on("snapshot", (data) => update(data as EntryProgress));
	on("phase", (data) => {
		// oxlint-disable-next-line typescript/no-unsafe-type-assertion -- as above
		if (state) update(applyChange(state, data as PhaseChange));
	});
	on("done", stop);
	on("gone", () => {
		stop();
		handlers.onGone?.();
	});
	stream.addEventListener("error", () => {
		// CLOSED: the browser gave up (an error status); otherwise it retries.
		if (stream.readyState === Source.CLOSED) stop();
	});
	return { close: stop };
}

/** A leaf's phases as "preview:analysis" statuses: what a leaf page shows. */
export const leafState = (leaf: LeafProgress) => `${leaf.preview.status}:${leaf.analysis.status}`;

/** One followed view of an entry's progress, for follow. */
export interface ProgressView {
	/** The state the page shows now, as the key function spells it. */
	shown(): string;
	/** Fetches the markup again; settles once the page shows it. */
	refresh(): Promise<void>;
	/** The entry was deleted or is no longer visible. */
	gone(): void;
}

/** One leaf as a page shows it, for watchLeaf. */
export type LeafView = ProgressView;

/**
 * Follows the entry and refreshes the view whenever `keyOf(state)` differs
 * from what it shows. Each state is fetched once, so markup that settles
 * differently (a stored analysis that no longer decodes shows as failed)
 * cannot loop, and one refresh runs at a time: a change that arrives during it
 * is fetched after it.
 */
function follow(
	options: WatchOptions,
	keyOf: (state: EntryProgress) => string | undefined,
	view: ProgressView,
): ProgressWatch {
	let wanted = "";
	let requested = "";
	let refreshing = false;
	const refresh = async () => {
		if (refreshing || wanted === requested || wanted === view.shown()) return;
		refreshing = true;
		requested = wanted;
		try {
			await view.refresh();
		} finally {
			refreshing = false;
		}
		void refresh();
	};
	return watchEntryProgress(options, {
		onState: (state) => {
			const key = keyOf(state);
			if (key === undefined) return;
			wanted = key;
			void refresh();
		},
		onGone: () => view.gone(),
	});
}

/**
 * Follows one leaf of the entry. A leaf of an older revision is not in the
 * live revision's progress, so it never refreshes.
 */
export function watchLeaf(
	options: WatchOptions,
	blueprintId: string,
	view: LeafView,
): ProgressWatch {
	return follow(
		options,
		(state) => {
			const leaf = state.leaves.find((candidate) => candidate.blueprint_id === blueprintId);
			return leaf && leafState(leaf);
		},
		view,
	);
}

/** The ids of the leaves whose preview is pending, sorted, comma-joined: what a book's cards show. */
export const pendingPreviews = (state: EntryProgress) =>
	state.leaves
		.filter((leaf) => leaf.preview.status === "pending")
		.map((leaf) => leaf.blueprint_id)
		.toSorted()
		.join(",");

/** Follows every leaf's preview, for a book: refreshes when the set of pending previews changes. */
export function watchPreviews(options: WatchOptions, view: ProgressView): ProgressWatch {
	return follow(options, pendingPreviews, view);
}

/** Where a quick preview stands: the first phase that failed for good, or whether every phase is ready. */
export function previewSettlement(state: EntryProgress): { failure?: PhaseState; ready: boolean } {
	for (const leaf of state.leaves) {
		if (leaf.preview.status === "failed") return { failure: leaf.preview, ready: false };
		if (leaf.analysis.status === "failed") return { failure: leaf.analysis, ready: false };
	}
	const ready =
		state.done &&
		state.leaves.every(
			(leaf) => leaf.preview.status === "ready" && leaf.analysis.status === "ready",
		);
	return { ready };
}
