<script lang="ts">
	// Build cost filters: a raw resource (item or fluid) of the blueprint's
	// build cost compared to an amount; rows are ANDed (cost_filters).
	import { untrack } from "svelte";
	import IconPicker from "@factorio-expo/ui/game/icon-picker/IconPicker.svelte";
	import type { SearchCostFilter } from "@lib/api/types";
	import { parseToken } from "@factorio-expo/ui/game/icon-token.ts";
	import { MAX_NUMERIC_FILTERS } from "@lib/search/query";
	import FilterRows from "@factorio-expo/ui/forms/FilterRows.svelte";

	type Row = {
		resource: string;
		initialIcon?: { value: string; style?: string };
		operator: SearchCostFilter["operator"];
		amount: number | undefined;
	};

	let {
		filters = [],
		initialIcons = [],
	}: {
		filters?: SearchCostFilter[];
		initialIcons?: { value: string; style?: string }[];
	} = $props();

	/** The raw cost is at normal quality, so a picked quality is ignored. */
	function parseResource(token: string) {
		const parsed = parseToken(token);
		if (!parsed || (parsed.type !== "item" && parsed.type !== "fluid")) return undefined;
		return { type: parsed.type, resource: parsed.name };
	}

	let rows = $state<Row[]>(
		untrack(() =>
			filters.map((filter, index) => ({
				resource: initialIcons[index]?.value ?? "",
				initialIcon: initialIcons[index],
				operator: filter.operator,
				amount: filter.amount,
			})),
		),
	);

	let root = $state<HTMLFieldSetElement>();
	let error = $state("");

	// Like ProductionFilters: a half-filled row stops the submit.
	$effect(() => {
		const form = root?.form;
		if (!form) return;
		const onSubmit = (event: SubmitEvent) => {
			error = "";
			for (const [index, row] of rows.entries()) {
				const hasResource = Boolean(parseResource(row.resource));
				const hasAmount = row.amount !== undefined && row.amount !== null;
				if (hasResource === hasAmount) continue;
				event.preventDefault();
				error = hasResource
					? "Enter an amount for each selected resource."
					: "Choose a resource for each build cost amount.";
				const rowElement = root?.querySelectorAll<HTMLElement>("[data-filter-row]")[index];
				(hasResource
					? rowElement?.querySelector<HTMLInputElement>('input[type="number"]')
					: rowElement?.querySelector<HTMLButtonElement>("button")
				)?.focus();
				return;
			}
		};
		form.addEventListener("submit", onSubmit);
		return () => form.removeEventListener("submit", onSubmit);
	});

	let serializedFilters = $derived(
		JSON.stringify(
			rows.flatMap((row) => {
				const resource = parseResource(row.resource);
				if (!resource || row.amount === undefined || !Number.isFinite(row.amount))
					return [];
				return [{ ...resource, operator: row.operator, amount: row.amount }];
			}),
		),
	);
</script>

<FilterRows
	bind:rows
	bind:fieldset={root}
	legend="Build cost"
	hint="(raw resources)"
	noun="build cost"
	empty="No build cost filters"
	max={MAX_NUMERIC_FILTERS}
	columns="minmax(4rem, auto) minmax(3.5rem, 0.4fr) minmax(5rem, 1fr)"
	narrowColumns="minmax(4rem, auto) minmax(3.5rem, 0.5fr) minmax(5rem, 1fr)"
	newRow={() => ({ resource: "", operator: "<", amount: undefined })}
>
	{#snippet row(item, index)}
		<div>
			<IconPicker
				kind={["item", "fluid"]}
				bind:value={item.resource}
				initialIcon={item.initialIcon}
				label={`Choose resource ${index + 1}`}
			/>
		</div>
		<select
			aria-label={`Amount comparison ${index + 1}`}
			bind:value={item.operator}
			data-cost-operator
		>
			<option value=">">&gt;</option>
			<option value="=">=</option>
			<option value="<">&lt;</option>
		</select>
		<input
			type="number"
			min="0"
			step="any"
			placeholder="Amount"
			aria-label={`Amount ${index + 1}`}
			bind:value={item.amount}
			data-cost-amount
		/>
	{/snippet}
	{#if error}
		<p role="alert" class="filter-error">{error}</p>
	{/if}
	<input type="hidden" name="cost_filters" value={serializedFilters} />
</FilterRows>

<style>
	.filter-error {
		margin: 0.5rem 0 0;
		color: var(--pico-del-color);
	}
</style>
