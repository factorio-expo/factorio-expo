<script lang="ts">
	// The challenge editor's import rule (challenge.Rules imports), written
	// into the hidden `imports_json` field: empty when imports are not limited,
	// else the materials the blueprint may take from outside. Everything else
	// has to be made inside the blueprint, which is how a challenge makes sure
	// the intermediates are built and not just the end product.
	import { untrack } from "svelte";
	import X from "@lucide/svelte/icons/x";
	import IconPicker from "@factorio-expo/ui/game/icon-picker/IconPicker.svelte";
	import { gameIcon } from "@factorio-expo/ui/game/game-icons.ts";
	import type { ChallengeImportRule } from "@lib/api/types";
	import { IMPORT_PRESETS, importNames, importRule, withPreset } from "@lib/challenge-imports";

	let { imports }: { imports?: ChallengeImportRule } = $props();

	let limited = $state(untrack(() => imports !== undefined));
	let names = $state<string[]>(untrack(() => importNames(imports)));

	// The picker lists items and fluids by name; the chips need each one's icon.
	const iconStyle = (name: string) => gameIcon("item", name) ?? gameIcon("fluid", name);

	let serialized = $derived(limited ? JSON.stringify(importRule(names)) : "");
</script>

<fieldset class="imports">
	<legend>Imports</legend>
	<label>
		<input type="checkbox" bind:checked={limited} />
		Limit what the blueprint may import
	</label>
	<p class="hint">
		The analysis reports any shortfall as an import, so everything not listed here has to be
		made inside the blueprint at the rate it is used. List only the starting materials: plates,
		coal, water, crude oil.
	</p>
	{#if limited}
		<div class="picker">
			<IconPicker
				kind={["item", "fluid"]}
				multiple
				bind:selected={names}
				label="Allowed imports"
				placeholder="Choose the allowed imports"
			/>
		</div>
		<div class="presets">
			{#each IMPORT_PRESETS as preset (preset.label)}
				<button
					type="button"
					class="secondary outline"
					onclick={() => (names = withPreset(names, preset.names))}
				>
					+ {preset.label}
				</button>
			{/each}
			{#if names.length}
				<button type="button" class="secondary outline" onclick={() => (names = [])}>
					Clear
				</button>
			{/if}
		</div>
		{#if names.length}
			<ul class="chips" aria-label="Allowed imports">
				{#each names as name (name)}
					<li>
						<span class="icon factorio-icon" style={iconStyle(name)} aria-hidden="true"
						></span>
						{name}
						<button
							type="button"
							class="remove"
							aria-label={`Remove ${name}`}
							onclick={() => (names = names.filter((other) => other !== name))}
						>
							<X size={14} />
						</button>
					</li>
				{/each}
			</ul>
		{:else}
			<p class="hint">
				<strong>No imports at all:</strong> everything is made inside the blueprint.
			</p>
		{/if}
	{/if}
	<input type="hidden" name="imports_json" value={serialized} />
</fieldset>

<style>
	.imports {
		margin-top: 1.25rem;
	}
	.hint {
		margin: 0.25rem 0 0.75rem;
		font-size: 0.875rem;
		color: var(--pico-muted-color);
	}
	.picker {
		max-width: 34rem;
	}
	.presets {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
		margin: 0.75rem 0;
	}
	.presets button {
		width: auto;
		margin: 0;
		padding: 0.25rem 0.75rem;
		font-size: 0.875rem;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.chips li {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		margin: 0;
		padding: 0.15rem 0.35rem 0.15rem 0.5rem;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: var(--pico-border-radius);
		font-size: 0.875rem;
	}
	.remove {
		display: inline-flex;
		width: auto;
		margin: 0;
		padding: 0.15rem;
		border: 0;
		background: none;
		color: var(--pico-muted-color);
	}
</style>
