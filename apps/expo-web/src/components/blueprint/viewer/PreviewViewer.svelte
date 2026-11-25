<script lang="ts">
	// The full-size preview's viewer (BlueprintPreview mounts it when its modal
	// first opens): the render in a stage you zoom and pan with the mouse
	// (wheel, drag, double click), touch (drag, pinch), the keyboard or the
	// buttons; a revision timeline when the entry is a single blueprint with
	// several revisions; and the leaf's author notes as numbered pins over the
	// image with a list below. The entry's editor adds a note with "Add note"
	// and a click on the image; pins are stored as fractions of the image, so
	// they follow it at every zoom. Notes of older revisions are read-only.
	import { assetUrl } from "@lib/api/public-asset";
	import { onMount, tick, untrack } from "svelte";
	import ChevronLeft from "@lucide/svelte/icons/chevron-left";
	import ChevronRight from "@lucide/svelte/icons/chevron-right";
	import Keyboard from "@lucide/svelte/icons/keyboard";
	import Maximize from "@lucide/svelte/icons/maximize";
	import MapPin from "@lucide/svelte/icons/map-pin";
	import Pencil from "@lucide/svelte/icons/pencil";
	import Trash2 from "@lucide/svelte/icons/trash-2";
	import ZoomIn from "@lucide/svelte/icons/zoom-in";
	import ZoomOut from "@lucide/svelte/icons/zoom-out";
	import IconPicker from "@factorio-expo/ui/game/icon-picker/IconPicker.svelte";
	import { type BlueprintNote, type HistoryStep } from "@lib/api";
	import { browserApi, runAction } from "@lib/browser-action";
	import { formatRichText } from "@factorio-expo/ui/game/rich-text.ts";
	import { stripRichText } from "@factorio-expo/ui/game/rich-text.ts";
	import {
		centerOn,
		clampView,
		fitView,
		panBy,
		pinchView,
		toFraction,
		toStage,
		zoomAt,
		zoomLimits,
		type Point,
		type Size,
		type View,
	} from "./panzoom";

	/** Matches the API's `catalog.MaxNoteRunes`. */
	const MAX_NOTE_LENGTH = 500;
	const BUTTON_ZOOM = 1.5;
	/** A press that moves less than this many pixels is a click, not a pan. */
	const CLICK_SLOP = 5;

	let {
		entryId,
		blueprintId,
		revisionId,
		details,
		onnotes,
		firstNotes,
		onimageerror,
		onstep,
	}: {
		/** The modal's facts and download buttons, shown between the timeline and the notes. */
		details?: HTMLElement;
		/** The page's own leaf's notes, as loaded and after every change (the page's thumbnail shows them). */
		onnotes?: (notes: BlueprintNote[]) => void;
		/** The page's own leaf's notes, already being fetched by the thumbnail (used once, instead of a second request). */
		firstNotes?: ReturnType<ReturnType<typeof browserApi>["entries"]["listNotes"]>;
		entryId: string;
		/** The page's own leaf and its revision. */
		blueprintId: string;
		revisionId: string;
		/** The page's own image failed to load (the full file may be gone). */
		onimageerror: () => void;
		/** Another revision's step is shown (null: back on the page's own leaf). */
		onstep: (step: HistoryStep | null) => void;
	} = $props();

	const id = $props.id();

	// What BlueprintPreview tells us: the image to show for the page's own leaf.
	let ownSrc = $state("");
	/** Shows `src` for the page's own leaf (the full file, or its thumbnail while it is rendered again). */
	export function setSource(src: string) {
		ownSrc = src;
	}

	// --- The timeline ---
	let steps = $state<HistoryStep[]>([]);
	let activeRevision = $state(revisionId);
	const showTimeline = $derived(
		steps.length > 1 && steps.some((s) => s.revision_id === revisionId),
	);
	const activeIndex = $derived(steps.findIndex((s) => s.revision_id === activeRevision));
	const activeStep = $derived(activeIndex >= 0 ? steps[activeIndex] : undefined);
	const isOwn = $derived(activeRevision === revisionId);
	const activeBlueprint = $derived(
		isOwn ? blueprintId : (activeStep?.blueprint_id ?? blueprintId),
	);
	// Another step's image that failed to load falls back to its thumbnail.
	let failedFull = $state<Record<string, true>>({});

	const src = $derived.by(() => {
		if (isOwn) return ownSrc;
		const preview = activeStep?.preview;
		if (!preview) return "";
		const useFull = preview.full_available && !failedFull[activeRevision];
		return assetUrl(useFull ? preview.url : preview.thumbnail_url);
	});

	function select(revision: string) {
		if (revision === activeRevision) return;
		activeRevision = revision;
		view = null;
		natural = null;
		stopNoting();
		onstep(
			revision === revisionId
				? null
				: (steps.find((s) => s.revision_id === revision) ?? null),
		);
		void loadNotes();
	}

	const step = (delta: number) => {
		const next = steps[activeIndex + delta];
		if (next) select(next.revision_id);
	};

	let helpOpen = $state(false);

	// --- The stage ---
	let stageEl: HTMLDivElement | undefined = $state();
	let stageW = $state(0);
	let stageH = $state(0);
	let natural = $state<Size | null>(null);
	let view = $state<View | null>(null);
	const stage = $derived<Size>({ w: stageW, h: stageH });
	const ready = $derived(natural !== null && stageW > 0 && stageH > 0);

	// A new image is shown fitted; a resized stage keeps the view inside it.
	$effect(() => {
		if (!natural || !stageW || !stageH) return;
		// Reading the view here would make this effect depend on what it writes.
		const current = untrack(() => view);
		view = current ? clampView(current, stage, natural) : fitView(stage, natural);
	});

	function imageLoaded(event: Event) {
		const image = event.currentTarget as HTMLImageElement;
		natural = { w: image.naturalWidth, h: image.naturalHeight };
		view = null;
	}

	function imageFailed() {
		if (isOwn) return onimageerror();
		if (!failedFull[activeRevision]) failedFull[activeRevision] = true;
	}

	const limits = $derived(natural && ready ? zoomLimits(stage, natural) : { min: 1, max: 1 });
	const atMin = $derived(!view || view.k <= limits.min + 1e-9);
	const atMax = $derived(!view || view.k >= limits.max - 1e-9);

	function zoomBy(factor: number, at: Point = { x: stageW / 2, y: stageH / 2 }) {
		if (view && natural) view = zoomAt(view, factor, at, stage, natural);
	}

	function actualSize() {
		if (view) zoomBy(1 / view.k);
	}

	function fit() {
		if (natural) view = fitView(stage, natural);
	}

	// Wheel zoom needs a non-passive listener (Svelte's onwheel is passive).
	function onWheel(event: WheelEvent) {
		if (!view || !natural) return;
		event.preventDefault();
		zoomBy(Math.exp(-event.deltaY * (event.deltaMode === 1 ? 0.05 : 0.0015)), local(event));
	}

	$effect(() => {
		const el = stageEl;
		if (!el) return;
		el.addEventListener("wheel", onWheel, { passive: false });
		return () => el.removeEventListener("wheel", onWheel);
	});

	function local(event: { clientX: number; clientY: number }): Point {
		const rect = stageEl!.getBoundingClientRect();
		return { x: event.clientX - rect.left, y: event.clientY - rect.top };
	}

	const pointers = new Map<number, Point>();
	let pan: { last: Point; origin: Point; moved: boolean } | null = null;
	let pinch: { start: View; ids: [number, number]; a0: Point; b0: Point } | null = null;

	function pointerDown(event: PointerEvent) {
		if (!view || (event.pointerType === "mouse" && event.button !== 0)) return;
		stageEl!.setPointerCapture(event.pointerId);
		const at = local(event);
		pointers.set(event.pointerId, at);
		if (pointers.size === 2) {
			const [a, b] = [...pointers.entries()];
			pinch = { start: view, ids: [a![0], b![0]], a0: a![1], b0: b![1] };
			pan = null;
		} else if (pointers.size === 1) {
			pan = { last: at, origin: at, moved: false };
		}
	}

	function pointerMove(event: PointerEvent) {
		if (!pointers.has(event.pointerId) || !view || !natural) return;
		const at = local(event);
		pointers.set(event.pointerId, at);
		if (pinch) {
			const a = pointers.get(pinch.ids[0]);
			const b = pointers.get(pinch.ids[1]);
			if (a && b) view = pinchView(pinch.start, pinch.a0, pinch.b0, a, b, stage, natural);
		} else if (pan) {
			if (Math.hypot(at.x - pan.origin.x, at.y - pan.origin.y) > CLICK_SLOP) pan.moved = true;
			if (pan.moved) view = panBy(view, at.x - pan.last.x, at.y - pan.last.y, stage, natural);
			pan.last = at;
		}
	}

	function pointerUp(event: PointerEvent) {
		if (!pointers.has(event.pointerId)) return;
		const clicked = pan && !pan.moved && pointers.size === 1 && event.type === "pointerup";
		const at = local(event);
		pointers.delete(event.pointerId);
		if (pinch) {
			pinch = null;
			// A finger left on the stage carries on as a pan.
			const rest = [...pointers.values()][0];
			pan = rest ? { last: rest, origin: rest, moved: true } : null;
		} else if (pointers.size === 0) {
			pan = null;
		}
		if (clicked && placing) placeDraft(at);
	}

	function doubleClick(event: MouseEvent) {
		if (!placing) zoomBy(2, local(event));
	}

	function stageKey(event: KeyboardEvent) {
		if (!view || !natural) return;
		if (event.ctrlKey || event.metaKey || event.altKey) return;
		const stepPx = 80;
		// Arrows and WASD move the view the same way.
		const moves: Record<string, [number, number]> = {
			arrowleft: [stepPx, 0],
			arrowright: [-stepPx, 0],
			arrowup: [0, stepPx],
			arrowdown: [0, -stepPx],
			a: [stepPx, 0],
			d: [-stepPx, 0],
			w: [0, stepPx],
			s: [0, -stepPx],
		};
		const key = event.key.toLowerCase();
		const move = moves[key];
		if (move) view = panBy(view, move[0], move[1], stage, natural);
		else if (key === "+" || key === "=") zoomBy(BUTTON_ZOOM);
		else if (key === "-") zoomBy(1 / BUTTON_ZOOM);
		else if (key === "1") actualSize();
		else if (key === "0") fit();
		else return;
		event.preventDefault();
	}

	// --- Notes ---
	let notes = $state<BlueprintNote[]>([]);
	let notesLoaded = $state(false);
	$effect(() => {
		if (isOwn && notesLoaded) onnotes?.(notes);
	});
	let canEdit = $state(false);
	let selectedId = $state("");
	let placing = $state(false);
	let draft = $state<Point | null>(null);
	let draftBody = $state("");
	let editingId = $state("");
	let editBody = $state("");
	let busy = $state(false);
	let status = $state("");
	const draftId = `${id}-draft`;
	const editId = `${id}-edit`;

	async function loadNotes() {
		const blueprint = activeBlueprint;
		const first = isOwn ? firstNotes : undefined;
		firstNotes = undefined;
		const list = await runAction(
			{
				fallback: "Could not load the notes.",
				onUnauthorized: "ignore",
				report: (m) => (status = m),
			},
			() => first ?? browserApi().entries.listNotes(entryId, blueprint),
		);
		if (!list || blueprint !== activeBlueprint) return;
		notes = list.notes;
		notesLoaded = true;
		canEdit = list.can_edit;
		if (!notes.some((n) => n.id === selectedId)) selectedId = "";
	}

	onMount(() => {
		void loadNotes();
		void loadHistory();
	});

	async function loadHistory() {
		// The timeline is a nicety: a failure leaves the viewer without it.
		const history = await browserApi()
			.entries.history(entryId)
			.catch(() => undefined);
		if (history) steps = history.steps;
	}

	function stopNoting() {
		placing = false;
		draft = null;
		draftBody = "";
		editingId = "";
		status = "";
	}

	function startPlacing() {
		editingId = "";
		draft = null;
		draftBody = "";
		placing = !placing;
		status = placing ? "Click the image where the note belongs." : "";
	}

	async function placeDraft(at: Point) {
		if (!view || !natural) return;
		const fraction = toFraction(view, at, natural);
		if (fraction.x < 0 || fraction.x > 1 || fraction.y < 0 || fraction.y > 1) {
			status = "Click on the image itself.";
			return;
		}
		draft = fraction;
		placing = false;
		status = "";
		await tick();
		document.getElementById(draftId)?.focus();
	}

	function field(event: Event) {
		return (event.currentTarget as HTMLFormElement).elements.namedItem(
			"body",
		) as HTMLTextAreaElement;
	}

	async function saveDraft(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		const body = field(event).value.trim();
		if (!body) return;
		const point = draft;
		const blueprint = activeBlueprint;
		busy = true;
		const note = await runAction(
			{
				fallback: "Could not save the note.",
				onUnauthorized: "login",
				report: (m) => (status = m),
			},
			() =>
				browserApi().entries.createNote(entryId, blueprint, {
					x: point.x,
					y: point.y,
					body,
				}),
		);
		busy = false;
		if (!note) return;
		draft = null;
		draftBody = "";
		await loadNotes();
		selectedId = note.id;
	}

	function startEdit(note: BlueprintNote) {
		draft = null;
		placing = false;
		editingId = note.id;
		editBody = note.body;
		status = "";
		void tick().then(() => document.getElementById(editId)?.focus());
	}

	async function saveEdit(event: SubmitEvent) {
		event.preventDefault();
		const body = field(event).value.trim();
		if (!body || !editingId) return;
		const blueprint = activeBlueprint;
		busy = true;
		const saved = await runAction(
			{
				fallback: "Could not save the note.",
				onUnauthorized: "login",
				report: (m) => (status = m),
			},
			() => browserApi().entries.editNote(entryId, blueprint, editingId, { body }),
		);
		busy = false;
		if (!saved) return;
		editingId = "";
		await loadNotes();
	}

	async function removeNote(note: BlueprintNote) {
		if (!confirm(`Delete note ${note.number}?`)) return;
		const blueprint = activeBlueprint;
		busy = true;
		const done = await runAction(
			{
				fallback: "Could not delete the note.",
				onUnauthorized: "login",
				report: (m) => (status = m),
			},
			() =>
				browserApi()
					.entries.deleteNote(entryId, blueprint, note.id)
					.then(() => true),
		);
		busy = false;
		if (done) await loadNotes();
	}

	/** Selects a note and brings its pin into view, at least at one image pixel per screen pixel. */
	function focusNote(note: BlueprintNote) {
		selectedId = note.id;
		if (!view || !natural) return;
		view = centerOn(
			{ x: note.x, y: note.y },
			Math.max(view.k, Math.min(1, limits.max)),
			stage,
			natural,
		);
		stageEl?.scrollIntoView({ block: "nearest" });
	}

	// --- Pins: hover card and dragging ---
	let hoverId = $state("");
	/** The pin being dragged, at its pointer's image fractions. */
	let drag = $state<{ id: string; x: number; y: number } | null>(null);
	let pinPress: { id: string; origin: Point; moved: boolean } | null = null;
	/** A finished drag is followed by a click on the pin: that one selects nothing. */
	let dragged = false;
	const hovered = $derived(notes.find((n) => n.id === hoverId));
	const movable = $derived(canEdit && isOwn);

	const pinPosition = (note: Point & { id?: string }) => {
		const point = drag && drag.id === note.id ? drag : note;
		const at = view && natural ? toStage(view, point, natural) : { x: 0, y: 0 };
		return `left:${at.x}px;top:${at.y}px`;
	};

	/** The hover card sits above its pin (below near the top edge), kept inside the stage. */
	const cardPosition = (note: BlueprintNote) => {
		const at = view && natural ? toStage(view, note, natural) : { x: 0, y: 0 };
		const left = Math.min(Math.max(at.x, 110), Math.max(110, stageW - 110));
		return at.y < 90
			? `left:${left}px;top:${at.y + 22}px`
			: `left:${left}px;top:${at.y - 18}px;translate:-50% -100%`;
	};

	function pinPressed(event: PointerEvent, note: BlueprintNote) {
		event.stopPropagation();
		if (!movable || (event.pointerType === "mouse" && event.button !== 0)) return;
		(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
		pinPress = { id: note.id, origin: local(event), moved: false };
	}

	function pinMoved(event: PointerEvent) {
		if (!pinPress || !view || !natural) return;
		const at = local(event);
		if (
			!pinPress.moved &&
			Math.hypot(at.x - pinPress.origin.x, at.y - pinPress.origin.y) <= CLICK_SLOP
		)
			return;
		pinPress.moved = true;
		const fraction = toFraction(view, at, natural);
		drag = {
			id: pinPress.id,
			x: Math.min(1, Math.max(0, fraction.x)),
			y: Math.min(1, Math.max(0, fraction.y)),
		};
	}

	async function pinReleased(event: PointerEvent) {
		const press = pinPress;
		pinPress = null;
		if (!press?.moved || !drag || event.type === "pointercancel") {
			drag = null;
			return;
		}
		dragged = true;
		const { id: noteId, x, y } = drag;
		const blueprint = activeBlueprint;
		// Stay where it was dropped while the save runs.
		const moved = notes.find((n) => n.id === noteId);
		if (moved) Object.assign(moved, { x, y });
		drag = null;
		selectedId = noteId;
		const saved = await runAction(
			{
				fallback: "Could not move the note.",
				onUnauthorized: "login",
				report: (m) => (status = m),
			},
			() => browserApi().entries.editNote(entryId, blueprint, noteId, { x, y }),
		);
		if (!saved) await loadNotes();
	}

	function pinClicked(note: BlueprintNote) {
		if (dragged) dragged = false;
		else focusNote(note);
	}

	const fullDate = (iso: string) => new Date(iso).toLocaleDateString();

	/** Moves a node rendered by the page into this component's layout. */
	function adopt(host: HTMLElement, node: HTMLElement) {
		host.append(node);
	}
</script>

<div class="viewer">
	<div class="toolbar" role="toolbar" aria-label="Zoom">
		<button
			type="button"
			class="icon-button"
			aria-label="Zoom in"
			data-tooltip="Zoom in"
			disabled={atMax}
			onclick={() => zoomBy(BUTTON_ZOOM)}
		>
			<ZoomIn aria-hidden="true" />
		</button>
		<button
			type="button"
			class="icon-button"
			aria-label="Zoom out"
			data-tooltip="Zoom out"
			disabled={atMin}
			onclick={() => zoomBy(1 / BUTTON_ZOOM)}
		>
			<ZoomOut aria-hidden="true" />
		</button>
		<button
			type="button"
			class="icon-button actual-size"
			aria-label="Actual size (100%)"
			data-tooltip="Actual size (100%)"
			disabled={!view || Math.abs(view.k - 1) < 1e-9}
			onclick={actualSize}
		>
			1:1
		</button>
		<button
			type="button"
			class="icon-button"
			aria-label="Fit to view"
			data-tooltip="Fit to view"
			disabled={atMin}
			onclick={fit}
		>
			<Maximize aria-hidden="true" />
		</button>
		<span class="zoom-level" aria-live="off">{view ? `${Math.round(view.k * 100)}%` : ""}</span>
		{#if canEdit && isOwn}
			<button
				type="button"
				class="add-note"
				class:active={placing}
				aria-pressed={placing}
				disabled={busy}
				onclick={startPlacing}
			>
				<MapPin aria-hidden="true" />
				{placing ? "Cancel" : "Add note"}
			</button>
		{/if}
		<div
			class="help"
			role="presentation"
			onpointerenter={() => (helpOpen = true)}
			onpointerleave={() => (helpOpen = false)}
		>
			<button
				type="button"
				class="icon-button"
				aria-label="Keyboard and mouse shortcuts"
				aria-expanded={helpOpen}
				aria-controls={`${id}-help`}
				onclick={() => (helpOpen = !helpOpen)}
				onblur={() => (helpOpen = false)}
				onkeydown={(event) =>
					event.key === "Escape" &&
					helpOpen &&
					((helpOpen = false), event.stopPropagation())}
			>
				<Keyboard aria-hidden="true" />
			</button>
			{#if helpOpen}
				<div class="help-card" id={`${id}-help`} role="tooltip">
					<h5>Shortcuts</h5>
					<dl>
						<dt>
							<kbd>W</kbd><kbd>A</kbd><kbd>S</kbd><kbd>D</kbd> or <kbd>←</kbd><kbd
								>↑</kbd
							><kbd>↓</kbd><kbd>→</kbd>
						</dt>
						<dd>Pan</dd>
						<dt><kbd>+</kbd> / <kbd>-</kbd></dt>
						<dd>Zoom in / out</dd>
						<dt><kbd>1</kbd></dt>
						<dd>Actual size (100%)</dd>
						<dt><kbd>0</kbd></dt>
						<dd>Fit to view</dd>
						<dt>Mouse wheel</dt>
						<dd>Zoom at the pointer</dd>
						<dt>Drag</dt>
						<dd>Pan (or move a pin you own)</dd>
						<dt>Double click</dt>
						<dd>Zoom in</dd>
						<dt>Two fingers</dt>
						<dd>Pinch to zoom, drag to pan</dd>
					</dl>
					<p>Keys work while the image is focused.</p>
				</div>
			{/if}
		</div>
	</div>

	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<div
		class="stage"
		class:placing
		bind:this={stageEl}
		bind:clientWidth={stageW}
		bind:clientHeight={stageH}
		tabindex="0"
		role="application"
		aria-label="Blueprint preview. Drag to pan, scroll or pinch to zoom, plus and minus keys to zoom, arrow or WASD keys to pan."
		onpointerdown={pointerDown}
		onpointermove={pointerMove}
		onpointerup={pointerUp}
		onpointercancel={pointerUp}
		ondblclick={doubleClick}
		onkeydown={stageKey}
	>
		{#if src}
			<img
				{src}
				alt=""
				draggable="false"
				class:hidden={!ready}
				style={natural && view
					? `width:${natural.w}px;height:${natural.h}px;transform:translate(${view.x}px,${view.y}px) scale(${view.k})`
					: ""}
				class:crisp={view !== null && view.k >= 2}
				onload={imageLoaded}
				onerror={imageFailed}
			/>
		{:else if !isOwn}
			<p class="empty">This revision has no render.</p>
		{/if}
		{#if !ready && src}<span class="busy" aria-busy="true">Loading…</span>{/if}
		{#if ready}
			{#each notes as note (note.id)}
				<button
					type="button"
					class="pin"
					class:selected={note.id === selectedId}
					class:hovered={note.id === hoverId}
					class:movable
					class:dragging={drag?.id === note.id}
					style={pinPosition(note)}
					aria-label={stripRichText(note.body)
						? `Note ${note.number}: ${stripRichText(note.body)}`
						: `Note ${note.number}`}
					onpointerenter={() => (hoverId = note.id)}
					onpointerleave={() => (hoverId = "")}
					onfocus={() => (hoverId = note.id)}
					onblur={() => (hoverId = "")}
					onpointerdown={(event) => pinPressed(event, note)}
					onpointermove={pinMoved}
					onpointerup={pinReleased}
					onpointercancel={pinReleased}
					ondblclick={(event) => event.stopPropagation()}
					onclick={() => pinClicked(note)}
				>
					{note.number}
				</button>
			{/each}
			{#if hovered && !drag}
				<div class="pin-card" style={cardPosition(hovered)} role="tooltip">
					{@html formatRichText(hovered.body)}
				</div>
			{/if}
			{#if draft}
				<span class="pin draft" style={pinPosition(draft)} aria-hidden="true">
					<MapPin />
				</span>
			{/if}
		{/if}
	</div>

	{#if showTimeline}
		<nav class="timeline" aria-label="Revisions">
			<button
				type="button"
				class="icon-button"
				aria-label="Previous revision"
				disabled={activeIndex <= 0}
				onclick={() => step(-1)}
			>
				<ChevronLeft aria-hidden="true" />
			</button>
			<ol>
				{#each steps as item (item.revision_id)}
					<li>
						<button
							type="button"
							class="stop"
							class:active={item.revision_id === activeRevision}
							aria-current={item.revision_id === activeRevision ? "step" : undefined}
							title={`Revision ${item.version_number}, ${fullDate(item.created_at)}${item.note ? `: ${item.note}` : ""}`}
							onclick={() => select(item.revision_id)}
						>
							<span class="dot" aria-hidden="true"></span>
							<span class="label">v{item.version_number}</span>
						</button>
					</li>
				{/each}
			</ol>
			<button
				type="button"
				class="icon-button"
				aria-label="Next revision"
				disabled={activeIndex >= steps.length - 1}
				onclick={() => step(1)}
			>
				<ChevronRight aria-hidden="true" />
			</button>
		</nav>
		{#if activeStep?.note}
			<p class="revision-note" data-revision-note>
				<strong>Revision {activeStep.version_number}:</strong>
				{activeStep.note}
			</p>
		{/if}
	{/if}

	{#if details}<div class="details-slot" use:adopt={details}></div>{/if}

	{#if draft}
		<form class="note-form draft-form" onsubmit={saveDraft}>
			<div class="field-head">
				<label for={draftId}>New note</label>
				<IconPicker targetId={draftId} label="Insert a game icon" />
			</div>
			<textarea
				id={draftId}
				name="body"
				rows="3"
				maxlength={MAX_NOTE_LENGTH}
				bind:value={draftBody}
				placeholder="Overflow logic here. Icons like [item=iron-plate] work."></textarea>
			<div class="form-row">
				<small>{draftBody.length}/{MAX_NOTE_LENGTH}</small>
				<span class="spacer"></span>
				<button type="button" class="secondary" onclick={() => (draft = null)}
					>Cancel</button
				>
				<button type="submit" disabled={busy || !draftBody.trim()} aria-busy={busy}
					>Save note</button
				>
			</div>
		</form>
	{/if}

	<p class="status" role="status" aria-live="polite">{status}</p>

	{#if notes.length > 0 || canEdit}
		<section class="notes" aria-label="Notes">
			<h4>
				Notes {#if notes.length}<small>({notes.length})</small>{/if}
			</h4>
			{#if notes.length === 0}
				<p class="muted">No notes yet. Add one, then click the image where it belongs.</p>
			{/if}
			<ol>
				{#each notes as note (note.id)}
					<li
						class:selected={note.id === selectedId}
						class:editing={editingId === note.id}
					>
						{#if editingId === note.id}
							<span class="number" aria-hidden="true">{note.number}</span>
							<form class="note-form" onsubmit={saveEdit}>
								<div class="field-head">
									<label for={editId}>Note {note.number}</label>
									<IconPicker targetId={editId} label="Insert a game icon" />
								</div>
								<textarea
									id={editId}
									name="body"
									rows="3"
									maxlength={MAX_NOTE_LENGTH}
									bind:value={editBody}></textarea>
								<div class="form-row">
									<small>{editBody.length}/{MAX_NOTE_LENGTH}</small>
									<span class="spacer"></span>
									<button
										type="button"
										class="secondary"
										onclick={() => (editingId = "")}
									>
										Cancel
									</button>
									<button
										type="submit"
										disabled={busy || !editBody.trim()}
										aria-busy={busy}
									>
										Save
									</button>
								</div>
							</form>
						{:else}
							<button
								type="button"
								class="note-main"
								aria-label={`Show note ${note.number} on the image`}
								onpointerenter={() => (hoverId = note.id)}
								onpointerleave={() => (hoverId = "")}
								onclick={() => focusNote(note)}
							>
								<span class="number" aria-hidden="true">{note.number}</span>
								<span class="body">{@html formatRichText(note.body)}</span>
							</button>
							{#if canEdit && isOwn}
								<div class="note-actions">
									<button
										type="button"
										class="icon-button"
										aria-label={`Edit note ${note.number}`}
										data-tooltip="Edit"
										disabled={busy}
										onclick={() => startEdit(note)}
									>
										<Pencil aria-hidden="true" />
									</button>
									<button
										type="button"
										class="icon-button"
										aria-label={`Delete note ${note.number}`}
										data-tooltip="Delete"
										disabled={busy}
										onclick={() => void removeNote(note)}
									>
										<Trash2 aria-hidden="true" />
									</button>
								</div>
							{/if}
						{/if}
					</li>
				{/each}
			</ol>
		</section>
	{/if}
</div>

<style>
	.viewer {
		display: grid;
		gap: 0.75rem;
	}

	/* The site's icon button size: Pico's button width wins over the global :where() default. */
	.icon-button {
		width: 2.75rem;
		height: 2.75rem;
		margin: 0;
		padding: 0;
	}

	.details-slot {
		display: contents;
	}

	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.4rem;
	}

	.actual-size {
		font-size: 0.8rem;
		font-weight: 700;
	}

	.help {
		position: relative;
		margin-left: auto;
	}

	.add-note + .help {
		margin-left: 0;
	}

	.help-card {
		position: absolute;
		z-index: 10;
		top: calc(100% + 0.25rem);
		right: 0;
		width: max-content;
		min-width: min(26rem, 85vw);
		max-width: min(36rem, 90vw);
		padding: 0.6rem 0.8rem;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: var(--pico-border-radius);
		background: var(--pico-card-background-color, var(--pico-background-color));
		color: var(--pico-color);
		font-size: 0.85rem;
		box-shadow: 0 0.25rem 1rem rgb(0 0 0 / 35%);
	}

	.help-card h5 {
		margin: 0 0 0.4rem;
		font-size: 0.9rem;
	}

	.help-card dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 0.3rem 1rem;
		margin: 0;
	}

	.help-card dt {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.2rem;
		font-weight: 600;
	}

	.help-card dd {
		margin: 0;
		color: var(--pico-muted-color);
	}

	.help-card p {
		margin: 0.5rem 0 0;
		color: var(--pico-muted-color);
		font-size: 0.8rem;
	}

	.help-card kbd {
		padding: 0.05rem 0.35rem;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: 0.25rem;
		background: var(--pico-code-background-color, transparent);
		font-size: 0.75rem;
	}

	.zoom-level {
		min-width: 3.5rem;
		color: var(--pico-muted-color);
		font-size: 0.875rem;
		font-variant-numeric: tabular-nums;
	}

	.add-note {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		width: auto;
		margin: 0 0 0 auto;
		padding: 0.4rem 0.8rem;
		font-size: 0.9rem;
	}

	.add-note.active {
		--pico-background-color: var(--pico-del-color);
		--pico-border-color: var(--pico-del-color);
	}

	.stage {
		position: relative;
		overflow: hidden;
		height: min(60vh, 40rem);
		border-radius: var(--pico-border-radius);
		background: var(--pico-muted-border-color);
		touch-action: none;
		cursor: grab;
		user-select: none;
	}

	/* A maximized modal gives the stage the rest of its height. */
	:global(dialog[data-maximized]) .stage {
		height: max(20rem, calc(100dvh - 14rem));
	}

	.stage:active {
		cursor: grabbing;
	}

	.stage.placing {
		cursor: crosshair;
	}

	.stage:focus-visible {
		outline: 2px solid var(--pico-primary);
		outline-offset: 2px;
	}

	.stage img {
		position: absolute;
		top: 0;
		left: 0;
		max-width: none;
		height: auto;
		transform-origin: 0 0;
		-webkit-user-drag: none;
	}

	.stage img.hidden {
		visibility: hidden;
	}

	.stage img.crisp {
		image-rendering: pixelated;
	}

	.viewer .busy,
	.viewer .empty {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		margin: 0;
		color: var(--pico-muted-color);
	}

	.pin {
		position: absolute;
		z-index: 2;
		display: grid;
		place-items: center;
		width: 1.6rem;
		height: 1.6rem;
		margin: 0;
		padding: 0;
		translate: -50% -50%;
		border: 2px solid #fff;
		border-radius: 50%;
		background: var(--pico-primary-background);
		color: var(--pico-primary-inverse);
		font-size: 0.8rem;
		font-weight: 700;
		line-height: 1;
		box-shadow: 0 1px 4px rgb(0 0 0 / 55%);
		cursor: pointer;
	}

	.pin.movable {
		cursor: grab;
		touch-action: none;
	}

	.pin.dragging {
		z-index: 4;
		cursor: grabbing;
	}

	.pin.hovered:not(.selected) {
		width: 1.9rem;
		height: 1.9rem;
	}

	.pin-card {
		position: absolute;
		z-index: 5;
		width: max-content;
		max-width: min(20rem, calc(100% - 1rem));
		padding: 0.5rem 0.65rem;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: var(--pico-border-radius);
		background: var(--pico-card-background-color, var(--pico-background-color));
		color: var(--pico-color);
		font-size: 0.875rem;
		overflow-wrap: anywhere;
		box-shadow: 0 0.25rem 1rem rgb(0 0 0 / 35%);
		pointer-events: none;
		translate: -50% 0;
	}

	.pin.selected {
		z-index: 3;
		width: 2rem;
		height: 2rem;
		box-shadow:
			0 0 0 3px var(--pico-primary),
			0 1px 6px rgb(0 0 0 / 60%);
	}

	.pin.draft {
		pointer-events: none;
		background: var(--pico-del-color);
	}

	.pin.draft :global(svg) {
		width: 1rem;
		height: 1rem;
	}

	.timeline {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	/* The uploader's note on the shown revision; line breaks they typed stay. */
	.revision-note {
		margin: 0;
		font-size: 0.875rem;
		white-space: pre-line;
		overflow-wrap: anywhere;
	}

	.timeline ol {
		display: flex;
		flex: 1;
		min-width: 0;
		margin: 0;
		padding: 0.25rem 0.25rem 0.5rem;
		overflow-x: auto;
		list-style: none;
	}

	.timeline li {
		position: relative;
		flex: 1 0 3.5rem;
		min-width: 3.5rem;
		margin: 0;
		padding: 0;
		list-style: none;
		text-align: center;
	}

	/* The line through the dots, between neighbouring steps only. */
	.timeline li:not(:first-child)::before {
		position: absolute;
		top: 0.55rem;
		right: 50%;
		left: -50%;
		height: 2px;
		background: var(--pico-muted-border-color);
		content: "";
	}

	.stop {
		position: relative;
		display: grid;
		justify-items: center;
		gap: 0.25rem;
		width: 100%;
		margin: 0;
		padding: 0;
		border: 0;
		background: none;
		color: var(--pico-muted-color);
		font-size: 0.8rem;
		cursor: pointer;
	}

	.dot {
		position: relative;
		z-index: 1;
		width: 1.1rem;
		height: 1.1rem;
		border: 2px solid var(--pico-muted-color);
		border-radius: 50%;
		background: var(--pico-background-color);
	}

	.stop.active {
		color: var(--pico-color);
		font-weight: 700;
	}

	.stop.active .dot {
		border-color: var(--pico-primary);
		background: var(--pico-primary);
	}

	.stop:focus-visible .dot {
		outline: 2px solid var(--pico-primary);
		outline-offset: 2px;
	}

	.viewer .status {
		margin: 0;
		color: var(--pico-muted-color);
		font-size: 0.875rem;
	}

	.viewer .status:empty {
		display: none;
	}

	.notes h4 {
		margin: 0 0 0.5rem;
	}

	.notes h4 small,
	.viewer .muted {
		color: var(--pico-muted-color);
	}

	.viewer .muted {
		margin: 0 0 0.5rem;
		font-size: 0.9rem;
	}

	.notes ol {
		display: grid;
		gap: 0.4rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.notes li {
		display: flex;
		align-items: center;
		gap: 0.25rem;
		margin: 0;
		padding: 0;
		list-style: none;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: var(--pico-border-radius);
		background: var(--pico-card-background-color);
	}

	.notes li.selected {
		border-color: var(--pico-primary);
	}

	.notes li.editing {
		gap: 0.6rem;
		padding: 0.5rem 0.6rem;
		align-items: flex-start;
	}

	/* The whole row is the button that shows the note on the image. */
	.note-main {
		display: flex;
		flex: 1;
		align-items: center;
		gap: 0.6rem;
		min-width: 0;
		margin: 0;
		padding: 0.5rem 0.6rem;
		border: 0;
		border-radius: var(--pico-border-radius);
		background: transparent;
		/* Pico's button color is the inverse of its fill; the row is text on the card. */
		color: inherit;
		font-weight: inherit;
		text-align: left;
		box-shadow: none;
		cursor: pointer;
	}

	.note-main:hover {
		background: var(--pico-card-sectioning-background-color);
	}

	.note-main:focus-visible {
		outline: 2px solid var(--pico-primary);
		outline-offset: -2px;
	}

	.number {
		display: grid;
		flex: none;
		place-items: center;
		width: 1.6rem;
		height: 1.6rem;
		border-radius: 50%;
		background: var(--pico-primary-background);
		color: var(--pico-primary-inverse);
		font-size: 0.8rem;
		font-weight: 700;
		line-height: 1;
	}

	.body {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}

	.note-actions {
		display: flex;
		flex: none;
		align-items: center;
		gap: 0.1rem;
		padding-right: 0.25rem;
	}

	.note-form {
		display: grid;
		flex: 1;
		gap: 0.4rem;
		min-width: 0;
	}

	.note-form textarea {
		margin: 0;
	}

	.note-form label {
		margin: 0;
	}

	.draft-form {
		padding: 0.75rem;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: var(--pico-border-radius);
	}

	/* The label on the left, the icon picker at the textarea's top right. */
	.field-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
	}

	.form-row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.5rem;
	}

	.form-row .spacer {
		flex: 1;
	}

	.form-row button {
		width: auto;
		margin: 0;
	}
</style>
