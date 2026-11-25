<script lang="ts">
	// The owner's entries of one collection, inline: drag a row to reorder (mouse,
	// touch and keyboard, svelte-dnd-action as in the pinned entries' arranger),
	// each saved at once with PUT /api/v1/collections/{id}/entries, and a remove
	// button per row. Rows come from the server render (CollectionPage).
	import { dndzone, type DndEvent } from "svelte-dnd-action";
	import { flip } from "svelte/animate";
	import GripVertical from "@lucide/svelte/icons/grip-vertical";
	import X from "@lucide/svelte/icons/x";
	import { browserApi, runAction } from "@lib/browser-action";

	export interface Row {
		id: string;
		title: string;
		image?: string;
	}

	let { collectionId, initial }: { collectionId: string; initial: Row[] } = $props();

	const FLIP_MS = 150;
	const EMPTY = "This collection is empty. Add entries from their pages.";
	// svelte-ignore state_referenced_locally
	let rows = $state<Row[]>(initial);
	let saved = rows.map((row) => row.id);
	let statusText = $state(rows.length ? "" : EMPTY);
	let busy = $state(false);

	const report = (message: string) => (statusText = message);

	function consider(event: CustomEvent<DndEvent<Row>>) {
		rows = event.detail.items;
	}

	async function finalize(event: CustomEvent<DndEvent<Row>>) {
		rows = event.detail.items;
		const order = rows.map((row) => row.id);
		if (order.join() === saved.join()) return;
		busy = true;
		const ok = await runAction(
			{
				fallback: "Could not save the order.",
				messages: { 404: "Collection not found." },
				onUnauthorized: "login",
				report,
			},
			() =>
				browserApi()
					.collections.reorderEntries(collectionId, order)
					.then(() => true),
		);
		busy = false;
		if (ok) {
			saved = order;
			statusText = "";
		} else {
			rows = saved.map((id) => rows.find((row) => row.id === id)!).filter(Boolean);
		}
	}

	async function remove(row: Row) {
		busy = true;
		const ok = await runAction(
			{
				fallback: "Could not remove the entry.",
				messages: { 404: "Collection not found." },
				onUnauthorized: "login",
				report,
			},
			() =>
				browserApi()
					.collections.removeEntry(collectionId, row.id)
					.then(() => true),
		);
		busy = false;
		if (!ok) return;
		rows = rows.filter((other) => other.id !== row.id);
		saved = saved.filter((id) => id !== row.id);
		statusText = rows.length ? "" : EMPTY;
	}
</script>

{#if rows.length > 1}
	<p class="hint">
		Drag to reorder, or with the keyboard: Space on a row, the arrow keys, then Space again.
	</p>
{/if}
<ol
	class="entries"
	aria-label="Entries"
	aria-busy={busy}
	use:dndzone={{ items: rows, flipDurationMs: FLIP_MS, dropTargetStyle: {}, dragDisabled: busy }}
	onconsider={consider}
	onfinalize={(event) => void finalize(event)}
>
	{#each rows as row (row.id)}
		<li animate:flip={{ duration: FLIP_MS }} data-collection-entry={row.id}>
			<GripVertical aria-hidden="true" class="grip" />
			<a class="link" href={`/e/${row.id}`} draggable="false">
				{#if row.image}
					<img src={row.image} alt="" loading="lazy" draggable="false" />
				{:else}
					<span class="placeholder"></span>
				{/if}
				<strong>{row.title}</strong>
			</a>
			<button
				type="button"
				class="icon-button"
				aria-label={`Remove ${row.title}`}
				data-tooltip="Remove"
				data-placement="left"
				disabled={busy}
				onclick={() => void remove(row)}
			>
				<X aria-hidden="true" />
			</button>
		</li>
	{/each}
</ol>
<p role="status" aria-live="polite" class="status">{statusText}</p>

<style>
	.hint {
		color: var(--pico-muted-color);
		font-size: 0.875em;
	}
	.entries {
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
	.link {
		display: flex;
		flex: 1;
		align-items: center;
		gap: 0.75rem;
		min-width: 0;
		text-decoration: none;
	}
	img,
	.placeholder {
		flex: none;
		width: 6rem;
		aspect-ratio: 3 / 2;
		object-fit: contain;
		background: var(--pico-muted-border-color);
	}
	strong {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.status:empty {
		margin: 0;
	}
</style>
