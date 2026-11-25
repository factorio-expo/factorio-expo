<script lang="ts">
	// The owner's "Arrange pinned entries" dialog body: the pins as rows to drag
	// (svelte-dnd-action: mouse, touch and keyboard), each with an unpin button.
	// Loads the pins each time the dialog opens (ProfilePins sends
	// PINS_OPEN_EVENT), saves the whole order in one call, then re-renders the
	// page's pinned entries.
	import { assetUrl } from "@lib/api/public-asset";
	import { dndzone, type DndEvent } from "svelte-dnd-action";
	import { flip } from "svelte/animate";
	import GripVertical from "@lucide/svelte/icons/grip-vertical";
	import X from "@lucide/svelte/icons/x";
	import { browserApi, runAction } from "@lib/browser-action";
	import { PIN_REGIONS, refreshLiveRegions } from "@lib/live-regions";
	import { stripRichText } from "@factorio-expo/ui/game/rich-text.ts";
	import { PINS_OPEN_EVENT } from "./pins.ts";

	let { username }: { username: string } = $props();

	interface Row {
		id: string;
		title: string;
		image?: string;
	}

	const FLIP_MS = 150;
	let rows = $state<Row[]>([]);
	let loaded = $state(false);
	let saving = $state(false);
	let statusText = $state("");
	let root: HTMLElement;

	async function load() {
		loaded = false;
		statusText = "Loading...";
		const page = await runAction(
			{
				fallback: "Could not load your pinned entries.",
				onUnauthorized: "login",
				report: (message) => (statusText = message),
			},
			() => browserApi().users.pins(username),
		);
		if (!page) return;
		rows = page.entries.map((entry) => ({
			id: entry.id,
			title: stripRichText(entry.title || entry.label).trim() || "Untitled blueprint",
			image: entry.preview_url ? assetUrl(entry.preview_url) : undefined,
		}));
		loaded = true;
		statusText = rows.length ? "" : "Nothing is pinned.";
	}

	const onOpen = () => void load();
	$effect(() => {
		document.addEventListener(PINS_OPEN_EVENT, onOpen);
		return () => document.removeEventListener(PINS_OPEN_EVENT, onOpen);
	});

	function sort(event: CustomEvent<DndEvent<Row>>) {
		rows = event.detail.items;
	}

	function unpin(id: string) {
		rows = rows.filter((row) => row.id !== id);
	}

	async function save() {
		saving = true;
		const saved = await runAction(
			{
				fallback: "Could not save your pinned entries.",
				onUnauthorized: "login",
				report: (message) => (statusText = message),
			},
			() =>
				browserApi()
					.users.setPins(rows.map((row) => row.id))
					.then(() => true),
		);
		saving = false;
		if (!saved) return;
		await refreshLiveRegions(PIN_REGIONS);
		root.closest("dialog")?.close();
	}
</script>

<div class="arranger" bind:this={root}>
	<p class="hint">
		Drag to reorder, or with the keyboard: Space on a row, the arrow keys, then Space again.
	</p>
	<ol
		aria-label="Pinned entries"
		use:dndzone={{ items: rows, flipDurationMs: FLIP_MS, dropTargetStyle: {} }}
		onconsider={sort}
		onfinalize={sort}
	>
		{#each rows as row (row.id)}
			<li animate:flip={{ duration: FLIP_MS }} data-pin-row={row.id}>
				<GripVertical aria-hidden="true" class="grip" />
				{#if row.image}
					<img src={row.image} alt="" loading="lazy" />
				{:else}
					<span class="placeholder"></span>
				{/if}
				<span class="title">{row.title}</span>
				<button
					type="button"
					class="icon-button"
					aria-label={`Unpin ${row.title}`}
					data-tooltip="Unpin"
					data-placement="left"
					onclick={() => unpin(row.id)}
				>
					<X aria-hidden="true" />
				</button>
			</li>
		{/each}
	</ol>
	<p role="status" aria-live="polite">{statusText}</p>
	<footer>
		<button type="button" class="secondary" onclick={() => root.closest("dialog")?.close()}>
			Cancel
		</button>
		<button
			type="button"
			data-pins-save
			disabled={!loaded || saving}
			aria-busy={saving}
			onclick={() => void save()}
		>
			Save
		</button>
	</footer>
</div>

<style>
	.hint {
		color: var(--pico-muted-color);
		font-size: 0.875em;
	}
	ol {
		margin: 0 0 1rem;
		padding: 0;
		list-style: none;
	}
	li {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		padding: 0.5rem;
		margin-bottom: 0.4rem;
		list-style: none;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: var(--pico-border-radius);
		background: var(--pico-card-background-color);
		cursor: grab;
	}
	li :global(.grip) {
		flex: none;
		width: 1.15rem;
		height: 1.15rem;
		color: var(--pico-muted-color);
	}
	img,
	.placeholder {
		flex: none;
		width: 4.5rem;
		aspect-ratio: 3 / 2;
		object-fit: contain;
		background: var(--pico-muted-border-color);
	}
	.title {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	footer {
		display: flex;
		justify-content: flex-end;
		gap: 0.75rem;
	}
	footer button {
		width: auto;
		margin: 0;
	}
</style>
