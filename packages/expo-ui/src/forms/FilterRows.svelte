<script lang="ts" generics="Row">
	// The one dynamic filter-row list of the advanced search (production rates,
	// build cost, platform metrics): a legend with a labelled "Add" button, one
	// bordered row per filter with its own fields (the `row` snippet) and a
	// remove button. Adding focuses the new row's first field; removing moves
	// focus to the next row's remove button, else the previous one's, else
	// "Add", so the list is usable from the keyboard alone.
	import { tick, type Snippet } from "svelte";
	import Plus from "@lucide/svelte/icons/plus";
	import Trash from "@lucide/svelte/icons/trash-2";

	let {
		rows = $bindable(),
		fieldset = $bindable(),
		legend,
		hint,
		noun,
		empty,
		max,
		columns,
		narrowColumns = "minmax(0, 1fr) minmax(0, 1fr)",
		newRow,
		row,
		children,
	}: {
		rows: Row[];
		/** The rendered fieldset, for an island that validates on submit. */
		fieldset?: HTMLFieldSetElement;
		legend: string;
		/** Small text after the legend, e.g. the unit. */
		hint?: string;
		/** What one row filters on, for the button labels ("production rate"). */
		noun: string;
		/** Shown while there are no rows. */
		empty: string;
		max?: number;
		/** grid-template-columns of a row's fields (the remove button is added). */
		columns: string;
		/** The same below 40rem; the fields wrap onto more lines. */
		narrowColumns?: string;
		newRow: () => Row;
		row: Snippet<[Row, number]>;
		children?: Snippet;
	} = $props();

	let list = $state<HTMLElement>();
	let addButton = $state<HTMLButtonElement>();
	const full = $derived(max !== undefined && rows.length >= max);

	const focusable = "button, input, select, [tabindex]:not([tabindex='-1'])";

	async function add() {
		rows.push(newRow());
		await tick();
		const rowElements = list?.querySelectorAll<HTMLElement>("[data-filter-row]");
		rowElements?.[rowElements.length - 1]?.querySelector<HTMLElement>(focusable)?.focus();
	}

	async function remove(index: number) {
		rows = rows.filter((_, rowIndex) => rowIndex !== index);
		await tick();
		const removeButtons = list?.querySelectorAll<HTMLButtonElement>("[data-remove-filter]");
		const next = removeButtons?.[Math.min(index, (removeButtons?.length ?? 0) - 1)];
		(next ?? addButton)?.focus();
	}
</script>

<fieldset
	class="filter-rows"
	bind:this={fieldset}
	style:--filter-columns={columns}
	style:--filter-columns-narrow={narrowColumns}
>
	<legend>
		<span
			>{legend}
			{#if hint}<small>{hint}</small>{/if}</span
		>
		<button
			type="button"
			class="secondary outline add-filter"
			bind:this={addButton}
			disabled={full}
			aria-label={`Add a ${noun} filter`}
			onclick={add}
		>
			<Plus aria-hidden="true" size={16} />
			<span>Add</span>
		</button>
	</legend>
	<div class="filter-row-list" bind:this={list}>
		{#if rows.length === 0}
			<i>{empty}</i>
		{/if}
		{#each rows as item, index (index)}
			<div class="filter-row" data-filter-row>
				<div class="filter-row-fields">
					{@render row(item, index)}
				</div>
				<button
					type="button"
					class="icon-button remove-filter"
					data-remove-filter
					aria-label={`Remove ${noun} filter ${index + 1}`}
					data-tooltip="Remove"
					data-placement="left"
					onclick={() => remove(index)}
				>
					<Trash aria-hidden="true" />
				</button>
			</div>
		{/each}
	</div>
	{@render children?.()}
</fieldset>

<style>
	.filter-rows {
		grid-column: 1 / -1;
		min-width: 0;
		margin: 0;
	}

	.filter-rows legend {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.5rem;
		width: 100%;
		margin-bottom: 0.5rem;
	}

	.add-filter {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		width: auto;
		margin: 0;
		padding: 0.35rem 0.75rem;
		font-size: 0.875rem;
	}

	.filter-row-list {
		display: grid;
		gap: 0.75rem;
	}

	.filter-row {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.65rem;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: 0.5rem;
	}

	.filter-row-fields {
		display: grid;
		flex: 1;
		grid-template-columns: var(--filter-columns);
		align-items: end;
		gap: 0.5rem;
		min-width: 0;
	}

	.filter-row-fields > :global(*) {
		min-width: 0;
	}

	.filter-row-fields :global(select),
	.filter-row-fields :global(input) {
		margin: 0;
	}

	.remove-filter:hover,
	.remove-filter:focus-visible {
		--pico-color: var(--pico-del-color);
	}

	@media (max-width: 40rem) {
		.filter-row-fields {
			grid-template-columns: var(--filter-columns-narrow);
		}
	}
</style>
