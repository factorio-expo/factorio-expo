<script lang="ts">
	import { untrack } from "svelte";
	import type { SearchPlatformFilter } from "@lib/api/types";
	import FilterRows from "@factorio-expo/ui/forms/FilterRows.svelte";
	import { MAX_NUMERIC_FILTERS } from "@lib/search/query";

	type Row = {
		metric: SearchPlatformFilter["metric"];
		operator: SearchPlatformFilter["operator"];
		value: number | undefined;
	};

	let { filters = [] }: { filters?: SearchPlatformFilter[] } = $props();
	let rows = $state<Row[]>(untrack(() => filters.map((filter) => Object.assign({}, filter))));
	let serializedFilters = $derived(
		JSON.stringify(
			rows.flatMap((row) =>
				row.value === undefined || !Number.isFinite(row.value) || row.value < 0
					? []
					: [{ metric: row.metric, operator: row.operator, value: row.value }],
			),
		),
	);
</script>

<FilterRows
	bind:rows
	legend="Space platform metrics"
	noun="platform metric"
	empty="No platform metric filters"
	max={MAX_NUMERIC_FILTERS}
	columns="minmax(10rem, 1fr) minmax(4rem, 0.4fr) minmax(6rem, 0.8fr)"
	narrowColumns="minmax(0, 1fr) minmax(4rem, 0.35fr)"
	newRow={() => ({ metric: "weight_tons", operator: ">", value: undefined })}
>
	{#snippet row(item, index)}
		<select aria-label={`Platform metric ${index + 1}`} bind:value={item.metric}>
			<option value="weight_tons">Weight (tons)</option>
			<option value="thrust_mn">Thrust (MN)</option>
			<option value="max_speed_km_s">Maximum speed (km/s)</option>
		</select>
		<select aria-label={`Comparison ${index + 1}`} bind:value={item.operator}>
			<option value=">">&gt;</option>
			<option value="=">=</option>
			<option value="<">&lt;</option>
		</select>
		<input
			type="number"
			min="0"
			step="any"
			placeholder="Value"
			aria-label={`Platform ${item.metric} value`}
			bind:value={item.value}
			data-platform-value
		/>
	{/snippet}
	<input type="hidden" name="platform_filters" value={serializedFilters} />
</FilterRows>
