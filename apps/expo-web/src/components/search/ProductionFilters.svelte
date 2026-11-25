<script lang="ts">
	import { untrack } from "svelte";
	import IconPicker from "@factorio-expo/ui/game/icon-picker/IconPicker.svelte";
	import type { SearchProductionFilter } from "@lib/api/types";
	import { hasQuality, parseToken } from "@factorio-expo/ui/game/icon-token.ts";
	import { MAX_NUMERIC_FILTERS, PRODUCTION_DIRECTIONS } from "@lib/search/query";
	import FilterRows from "@factorio-expo/ui/forms/FilterRows.svelte";

	type Operator = SearchProductionFilter["operator"];
	type Direction = SearchProductionFilter["direction"];
	type ProductionRow = {
		product: string;
		initialIcon?: { value: string; style?: string };
		operator: Operator;
		rate: number | undefined;
		direction: Direction;
	};

	let {
		filters = [],
		initialIcons = [],
	}: {
		filters?: SearchProductionFilter[];
		initialIcons?: { value: string; style?: string }[];
	} = $props();

	function parseProduct(token: string) {
		const parsed = parseToken(token);
		if (!parsed || (parsed.type !== "item" && parsed.type !== "fluid")) return undefined;
		// An explicit quality (even "normal") is a filter; no suffix means any
		// quality. Fluids have none (hasQuality is false for them).
		return {
			type: parsed.type,
			product: parsed.name,
			quality: hasQuality(token) ? parsed.quality : undefined,
		};
	}

	let rows = $state<ProductionRow[]>(
		untrack(() =>
			filters.map((filter, index) => ({
				product: initialIcons[index]?.value ?? "",
				initialIcon: initialIcons[index],
				operator: filter.operator,
				rate: filter.rate,
				direction: filter.direction,
			})),
		),
	);

	let root = $state<HTMLFieldSetElement>();
	let error = $state("");

	// The island validates its own rows when the surrounding form submits.
	// Rows inside a disabled fieldset (the hidden blueprint-type panel) are
	// not submitted, so they are not validated either.
	$effect(() => {
		const form = root?.form;
		if (!form) return;
		const onSubmit = (event: SubmitEvent) => {
			error = "";
			if (root?.parentElement?.closest("fieldset[disabled]")) return;
			for (const [index, row] of rows.entries()) {
				const hasProduct = Boolean(parseProduct(row.product));
				const hasRate = row.rate !== undefined && row.rate !== null;
				if (!hasProduct && !hasRate) continue;
				if (hasProduct && hasRate) continue;
				event.preventDefault();
				error = hasProduct
					? "Enter a rate for each selected product."
					: "Choose a product for each production rate.";
				const rowElement = root?.querySelectorAll<HTMLElement>("[data-filter-row]")[index];
				(hasProduct
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
				const product = parseProduct(row.product);
				if (!product || row.rate === undefined || !Number.isFinite(row.rate)) return [];
				return [
					{
						...product,
						operator: row.operator,
						rate: row.rate,
						direction: row.direction,
					},
				];
			}),
		),
	);
</script>

<FilterRows
	bind:rows
	bind:fieldset={root}
	legend="Production rates"
	hint="(items per second)"
	noun="production rate"
	max={MAX_NUMERIC_FILTERS}
	empty="No production rate filters"
	columns="minmax(4rem, auto) minmax(9rem, 1fr) minmax(8rem, 1fr)"
	narrowColumns="minmax(4rem, auto) minmax(6rem, 1fr)"
	newRow={() => ({ product: "", operator: ">", rate: undefined, direction: "out" })}
>
	{#snippet row(item, index)}
		<div>
			<IconPicker
				kind={["item", "fluid"]}
				allowAny
				bind:value={item.product}
				initialIcon={item.initialIcon}
				label={`Choose product ${index + 1}`}
			/>
		</div>
		<div class="rate-fields">
			<select
				id={`production-operator-${index}`}
				aria-label={`Rate comparison ${index + 1}`}
				bind:value={item.operator}
				data-filter-operator
			>
				<option value=">">&gt;</option>
				<option value="=">=</option>
				<option value="<">&lt;</option>
			</select>
			<input
				id={`production-rate-${index}`}
				type="number"
				min="0"
				step="any"
				placeholder="Rate"
				aria-label={`Rate value ${index + 1}`}
				bind:value={item.rate}
			/>
		</div>
		<div class="direction">
			<select
				id={`production-direction-${index}`}
				aria-label={`Rate measure ${index + 1}`}
				title="Out and in are per second; per tile divides by the blueprint's footprint, per MW by the electricity it uses (blueprints only)"
				bind:value={item.direction}
				data-filter-direction
			>
				{#each PRODUCTION_DIRECTIONS as direction (direction.value)}
					<option value={direction.value}>{direction.label}</option>
				{/each}
			</select>
		</div>
	{/snippet}
	{#if error}
		<p role="alert" class="filter-error">{error}</p>
	{/if}
	<input type="hidden" name="production_filters" value={serializedFilters} />
</FilterRows>

<style>
	.filter-error {
		margin: 0.5rem 0 0;
		color: var(--pico-del-color);
	}

	.rate-fields {
		display: grid;
		grid-template-columns: minmax(3.5rem, 0.5fr) minmax(5rem, 1fr);
		gap: 0.35rem;
	}

	@media (max-width: 40rem) {
		.direction {
			grid-column: 2;
		}
	}
</style>
