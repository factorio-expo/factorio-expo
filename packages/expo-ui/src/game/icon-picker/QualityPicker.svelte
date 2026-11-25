<script lang="ts">
	import { gameIcon } from "../game-icons";
	import { qualityOptions } from "../qualities";

	let {
		qualities,
		quality,
		name,
		disabled = false,
		onselect,
	}: {
		qualities: { value: string; label: string }[];
		quality: string;
		name?: string;
		disabled?: boolean;
		onselect: (value: string) => void;
	} = $props();

	const qualityStyles = Object.fromEntries(
		["any", ...qualityOptions.map((option) => option.value)].map((value) => [
			value,
			gameIcon("quality", value),
		]),
	);
</script>

<div class="quality-picker">
	<span class="quality-picker-label">Quality</span>
	<input type="hidden" {name} value={quality} data-quality-value />
	<div class="quality-options" role="group" aria-label="Quality">
		{#each qualities as option (option.value)}
			<button
				type="button"
				class:outline={quality !== option.value}
				aria-pressed={quality === option.value}
				aria-label={option.label}
				title={option.label}
				{disabled}
				onclick={() => onselect(option.value)}
			>
				<span
					class="icon factorio-icon"
					style={qualityStyles[option.value || "any"]}
					aria-hidden="true"
				></span>
			</button>
		{/each}
	</div>
</div>

<style>
	.quality-picker {
		margin-top: 0.65rem;
	}
	.quality-picker-label {
		display: block;
		margin-bottom: 0.35rem;
		font-size: 0.875rem;
	}
	.quality-options {
		margin: 0;
	}
	.quality-options button {
		display: grid;
		place-items: center;
		min-width: 0;
		margin: 0;
		padding: 0.5rem 0.25rem;
	}
	.icon {
		width: 1.4rem;
		height: 1.4rem;
		font-size: 1.4rem;
	}
</style>
