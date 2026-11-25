import { describe, expect, it, vi } from "vitest";

import type { EntryProgress } from "@lib/api/types";
import {
	applyChange,
	previewSettlement,
	watchEntryProgress,
	watchLeaf,
	watchPreviews,
} from "./entry-progress";

const pending = { status: "pending" } as const;
const snapshot = (done = false): EntryProgress => ({
	entry_id: "e",
	revision_id: "r",
	leaves: [
		{ blueprint_id: "b", preview: pending, analysis: done ? { status: "ready" } : pending },
	],
	done,
});

/** A scriptable EventSource stand-in. */
class FakeSource extends EventTarget {
	static readonly CONNECTING = 0;
	static readonly OPEN = 1;
	static readonly CLOSED = 2;
	static last: FakeSource;
	readyState = FakeSource.OPEN;
	closed = false;
	constructor(
		readonly url: string,
		readonly init?: EventSourceInit,
	) {
		super();
		FakeSource.last = this;
	}
	close() {
		this.closed = true;
		this.readyState = FakeSource.CLOSED;
	}
	emit(name: string, data: unknown) {
		this.dispatchEvent(new MessageEvent(name, { data: JSON.stringify(data) }));
	}
	fail(readyState: number) {
		this.readyState = readyState;
		this.dispatchEvent(new Event("error"));
	}
}

describe("applyChange", () => {
	it("updates one phase and settles done", () => {
		const half = applyChange(snapshot(), {
			blueprint_id: "b",
			phase: "preview",
			state: { status: "failed", code: "render_failed" },
		});
		expect(half.leaves[0].preview).toEqual({ status: "failed", code: "render_failed" });
		expect(half.done).toBe(false);
		const all = applyChange(half, {
			blueprint_id: "b",
			phase: "analysis",
			state: { status: "ready" },
		});
		expect(all.done).toBe(true);
	});

	it("ignores other leaves", () => {
		const state = snapshot();
		const next = applyChange(state, {
			blueprint_id: "x",
			phase: "preview",
			state: { status: "ready" },
		});
		expect(next.leaves).toEqual(state.leaves);
	});
});

describe("watchEntryProgress", () => {
	const source = FakeSource as unknown as typeof EventSource;

	it("follows the stream until done and closes it", () => {
		const states: EntryProgress[] = [];
		watchEntryProgress(
			{ eventsUrl: "/events", EventSource: source },
			{ onState: (state) => states.push(state) },
		);
		const stream = FakeSource.last;
		expect(stream.init?.withCredentials).toBe(true);
		stream.emit("snapshot", snapshot());
		stream.emit("phase", { blueprint_id: "b", phase: "preview", state: { status: "ready" } });
		stream.emit("phase", { blueprint_id: "b", phase: "analysis", state: { status: "ready" } });
		expect(states.map((state) => state.done)).toEqual([false, false, true]);
		expect(stream.closed).toBe(true);
	});

	it("reports gone", () => {
		const onGone = vi.fn<() => void>();
		watchEntryProgress(
			{ eventsUrl: "/events", EventSource: source },
			{ onState: vi.fn<(state: EntryProgress) => void>(), onGone },
		);
		FakeSource.last.emit("snapshot", snapshot());
		FakeSource.last.emit("gone", { entry_id: "e" });
		expect(onGone).toHaveBeenCalledOnce();
		expect(FakeSource.last.closed).toBe(true);
	});

	it("leaves reconnecting to the browser and stops once it gives up", () => {
		const states: EntryProgress[] = [];
		watchEntryProgress(
			{ eventsUrl: "/events", EventSource: source },
			{ onState: (state) => states.push(state) },
		);
		const stream = FakeSource.last;
		stream.fail(FakeSource.CONNECTING);
		expect(stream.closed).toBe(false);
		stream.emit("snapshot", snapshot());
		expect(states).toHaveLength(1);
		stream.fail(FakeSource.CLOSED);
		stream.emit("snapshot", snapshot());
		expect(states).toHaveLength(1);
	});
});

const leafSource = FakeSource as unknown as typeof EventSource;
const leafAt = (preview: "pending" | "ready", analysis: "pending" | "ready"): EntryProgress => ({
	entry_id: "e",
	revision_id: "r",
	leaves: [{ blueprint_id: "b", preview: { status: preview }, analysis: { status: analysis } }],
	done: false,
});
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
const noop = () => {};

describe("watchLeaf", () => {
	it("refreshes once per new state and ignores what the page already shows", async () => {
		let shown = "pending:pending";
		const refresh = vi.fn<() => Promise<void>>(async () => {
			shown = "ready:pending";
		});
		watchLeaf({ eventsUrl: "/events", EventSource: leafSource }, "b", {
			shown: () => shown,
			refresh,
			gone: noop,
		});
		FakeSource.last.emit("snapshot", leafAt("pending", "pending"));
		await flush();
		expect(refresh).not.toHaveBeenCalled();
		FakeSource.last.emit("snapshot", leafAt("ready", "pending"));
		await flush();
		FakeSource.last.emit("snapshot", leafAt("ready", "pending"));
		await flush();
		expect(refresh).toHaveBeenCalledTimes(1);
	});

	it("never loops on markup that settles differently", async () => {
		const refresh = vi.fn<() => Promise<void>>(async () => {});
		watchLeaf({ eventsUrl: "/events", EventSource: leafSource }, "b", {
			shown: () => "ready:failed",
			refresh,
			gone: noop,
		});
		FakeSource.last.emit("snapshot", leafAt("ready", "ready"));
		await flush();
		await flush();
		expect(refresh).toHaveBeenCalledTimes(1);
	});

	it("fetches a change that arrives during a refresh after it", async () => {
		const releases: (() => void)[] = [];
		const refresh = vi.fn<() => Promise<void>>(
			() => new Promise<void>((resolve) => releases.push(resolve)),
		);
		watchLeaf({ eventsUrl: "/events", EventSource: leafSource }, "b", {
			shown: () => "pending:pending",
			refresh,
			gone: noop,
		});
		FakeSource.last.emit("snapshot", leafAt("ready", "pending"));
		FakeSource.last.emit("phase", {
			blueprint_id: "b",
			phase: "analysis",
			state: { status: "ready" },
		});
		expect(refresh).toHaveBeenCalledTimes(1);
		releases[0]();
		await flush();
		expect(refresh).toHaveBeenCalledTimes(2);
	});

	it("ignores a leaf of another revision and reports a gone entry", () => {
		const refresh = vi.fn<() => Promise<void>>(async () => {});
		const gone = vi.fn<() => void>();
		watchLeaf({ eventsUrl: "/events", EventSource: leafSource }, "other", {
			shown: () => "pending:pending",
			refresh,
			gone,
		});
		FakeSource.last.emit("snapshot", leafAt("ready", "ready"));
		expect(refresh).not.toHaveBeenCalled();
		FakeSource.last.emit("gone", {});
		expect(gone).toHaveBeenCalledOnce();
	});
});

const book = (...previews: ("pending" | "ready")[]): EntryProgress => ({
	entry_id: "e",
	revision_id: "r",
	leaves: previews.map((preview, index) => ({
		blueprint_id: `b${index}`,
		preview: { status: preview },
		analysis: { status: "ready" },
	})),
	done: false,
});

describe("watchPreviews", () => {
	it("refreshes when the set of pending previews changes, not on other changes", async () => {
		let shown = "b0,b1";
		const refresh = vi.fn<() => Promise<void>>(async () => {
			shown = "b1";
		});
		watchPreviews(
			{ eventsUrl: "/events", EventSource: leafSource },
			{
				shown: () => shown,
				refresh,
				gone: noop,
			},
		);
		FakeSource.last.emit("snapshot", book("pending", "pending"));
		await flush();
		expect(refresh).not.toHaveBeenCalled();
		FakeSource.last.emit("phase", {
			blueprint_id: "b0",
			phase: "preview",
			state: { status: "ready" },
		});
		await flush();
		expect(refresh).toHaveBeenCalledTimes(1);
	});
});

describe("previewSettlement", () => {
	it("names the first failed phase", () => {
		const state = applyChange(snapshot(), {
			blueprint_id: "b",
			phase: "analysis",
			state: { status: "failed", code: "analysis_failed" },
		});
		expect(previewSettlement(state)).toEqual({
			failure: { status: "failed", code: "analysis_failed" },
			ready: false,
		});
	});

	it("is ready only once every phase is ready", () => {
		expect(previewSettlement(snapshot())).toEqual({ ready: false });
		const ready = applyChange(snapshot(true), {
			blueprint_id: "b",
			phase: "preview",
			state: { status: "ready" },
		});
		expect(previewSettlement(ready)).toEqual({ ready: true });
	});
});
