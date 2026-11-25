<script lang="ts">
	import { gameIcon } from "../game-icons";
	import { iconPicker, type IconPickerKind } from "../data/icon-picker.data";
	import { parseToken, type IconToken } from "../icon-token";

	let {
		id,
		kind,
		query,
		quality,
		multiple,
		selected,
		current,
		onchoose,
	}: {
		id: string;
		kind: IconPickerKind | IconPickerKind[];
		query: string;
		quality: string;
		multiple: boolean;
		selected: string[];
		current: IconToken | undefined;
		onchoose: (token: string) => void;
	} = $props();

	let tabs: HTMLDivElement;
	let activeCategory = $state("");
	// Tabs are the game's item groups, rows its subgroups, in inventory order.
	// Several kinds merge tab by tab (items and fluids for production rates).
	let categories = $derived.by(() => {
		const merged = new Map<string, string[][]>();
		for (const each of Array.isArray(kind) ? kind : [kind]) {
			for (const { name, rows } of iconPicker[each]) {
				merged.set(name, [...(merged.get(name) ?? []), ...rows]);
			}
		}
		return [...merged].map(([name, rows]) => ({ name, rows }));
	});
	let category = $derived(
		categories.find(({ name }) => name === activeCategory)?.name ?? categories[0]?.name,
	);
	let rows = $derived(
		(categories.find(({ name }) => name === category)?.rows ?? [])
			.map((row) =>
				row.filter((token) =>
					token.toLowerCase().includes(query.trim().toLowerCase().replaceAll(" ", "-")),
				),
			)
			.filter((row) => row.length > 0),
	);

	function tabKeydown(event: KeyboardEvent, index: number) {
		if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
		event.preventDefault();
		const next =
			event.key === "Home"
				? 0
				: event.key === "End"
					? categories.length - 1
					: (index + (event.key === "ArrowRight" ? 1 : -1) + categories.length) %
						categories.length;
		activeCategory = categories[next].name;
		tabs.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus();
	}
</script>

<div class="icon-picker-tabs" role="tablist" aria-label="Icon categories" bind:this={tabs}>
	{#each categories as { name }, index (name)}
		<button
			id={`${id}-tab-${index}`}
			type="button"
			class="outline secondary icon-picker-tab"
			role="tab"
			aria-selected={category === name}
			aria-controls={`${id}-panel`}
			tabindex={category === name ? 0 : -1}
			onclick={() => (activeCategory = name)}
			onkeydown={(event) => tabKeydown(event, index)}>{name.replaceAll("-", " ")}</button
		>
	{/each}
</div>
<div
	id={`${id}-panel`}
	class="icon-picker-panel"
	role="tabpanel"
	aria-labelledby={category
		? `${id}-tab-${categories.findIndex(({ name }) => name === category)}`
		: undefined}
	tabindex="0"
>
	{#each rows as row}
		<div class="icon-picker-row">
			{#each row as token}
				{@const option = parseToken(token)}
				<button
					type="button"
					class="icon-button icon-picker-option"
					aria-label={token}
					aria-pressed={multiple
						? selected.includes(option?.name ?? "")
						: current?.type === option?.type && current?.name === option?.name}
					title={token}
					onclick={() => onchoose(token)}
				>
					{#if option && gameIcon(option.type, option.name)}
						<span
							class="icon factorio-icon"
							style={gameIcon(
								option.type,
								option.name,
								option.type === "fluid" ? "normal" : quality || "normal",
							)}
							aria-hidden="true"
						></span>
					{:else}
						<span class="icon-placeholder" aria-hidden="true"></span>
					{/if}
				</button>
			{/each}
		</div>
	{:else}
		<p role="status">No icons found.</p>
	{/each}
</div>

<style>
	.icon-picker-tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 0.25rem;
		margin-bottom: 0.65rem;
		border-bottom: 1px solid var(--pico-muted-border-color);
	}
	.icon-picker-tab {
		width: auto;
		margin: 0;
		padding: 0.35rem 0.55rem;
		border: 0;
		border-radius: var(--pico-border-radius) var(--pico-border-radius) 0 0;
		text-transform: capitalize;
	}
	.icon-picker-tab[aria-selected="true"] {
		border-bottom: 2px solid var(--pico-primary);
		color: var(--pico-primary);
	}
	.icon-picker-panel {
		max-height: 17rem;
		overflow: auto;
	}
	.icon-picker-row {
		display: flex;
		flex-wrap: wrap;
		gap: 0.25rem;
		margin-bottom: 0.35rem;
	}
	.icon-picker-option {
		display: grid;
		place-items: center;
		width: 2.25rem;
		height: 2.25rem;
		margin: 0;
		padding: 0.35rem;
	}
	.icon-picker-option[aria-pressed="true"] {
		outline: 2px solid var(--pico-primary);
		outline-offset: -2px;
	}
	.icon-placeholder {
		width: 1.4rem;
		height: 1.4rem;
		border: 1px dashed var(--pico-muted-color);
		border-radius: 0.2rem;
	}
	.icon {
		width: 1.4rem;
		height: 1.4rem;
		font-size: 1.4rem;
	}
</style>
