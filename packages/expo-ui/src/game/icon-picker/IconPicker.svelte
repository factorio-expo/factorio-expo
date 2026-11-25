<script lang="ts">
	import { onMount, tick } from "svelte";
	import ImagePlus from "@lucide/svelte/icons/image-plus";
	import { gameIcon } from "../game-icons";
	import type { IconPickerKind } from "../data/icon-picker.data";
	import { qualityOptions } from "../qualities";
	import IconGrid from "./IconGrid.svelte";
	import QualityPicker from "./QualityPicker.svelte";
	import { registerDropdown } from "../../utils/dropdown";
	import { formatToken, hasQuality, parseToken } from "../icon-token";

	let {
		kind = "icon",
		targetId,
		label = "Choose a game icon",
		value = $bindable(""),
		selected = $bindable<string[]>([]),
		quality = $bindable("normal"),
		name,
		qualityName,
		multiple = false,
		allowAny = false,
		placeholder = "Choose an icon",
		initialIcon,
	}: {
		kind?: IconPickerKind | IconPickerKind[];
		targetId?: string;
		label?: string;
		value?: string;
		selected?: string[];
		quality?: string;
		name?: string;
		qualityName?: string;
		multiple?: boolean;
		allowAny?: boolean;
		placeholder?: string;
		initialIcon?: { value: string; style?: string };
	} = $props();

	const id = $props.id();
	// "insert" adds a token to a text field, "multiple" toggles names in a
	// filter list, "single" owns one token in `value`.
	const mode = $derived<"insert" | "multiple" | "single">(
		multiple ? "multiple" : targetId ? "insert" : "single",
	);
	let qualities = $derived(
		allowAny ? [{ value: "", label: "Any quality" }, ...qualityOptions] : qualityOptions,
	);
	let root: HTMLDivElement;
	let trigger: HTMLButtonElement;
	let popover: HTMLDivElement;
	let search: HTMLInputElement;
	let isOpen = $state(false);
	let query = $state("");
	let error = $state("");
	let current = $derived(parseToken(value));
	let selectedStyle = $derived(
		current
			? (gameIcon(current.type, current.name, current.quality) ??
					(initialIcon?.value === value ? initialIcon.style : undefined))
			: undefined,
	);

	function qualifiedToken(token: string) {
		const option = parseToken(token);
		if (!option || option.type === "fluid" || !quality || (quality === "normal" && !allowAny))
			return token;
		return formatToken({ ...option, quality }, { keepNormal: true });
	}

	$effect(() => {
		if (mode === "single") {
			quality =
				current && current.type !== "fluid"
					? hasQuality(value)
						? current.quality
						: allowAny
							? ""
							: "normal"
					: allowAny
						? ""
						: "normal";
		}
	});

	// Inside a modal the popup would be clipped by its scroll area: it is fixed
	// to the viewport next to the trigger instead, and closes when anything
	// else scrolls.
	let floating = $state("");

	function place() {
		if (!root.closest("dialog")) {
			floating = "";
			return;
		}
		const rect = trigger.getBoundingClientRect();
		const margin = 16;
		const width = Math.min(34 * 16, window.innerWidth - 2 * margin);
		const anchor = mode === "insert" ? rect.right - width : rect.left;
		const left = Math.min(Math.max(margin, anchor), window.innerWidth - width - margin);
		const below = window.innerHeight - rect.bottom - margin;
		const above = rect.top - margin;
		const open =
			below >= 280 || below >= above
				? `top:${rect.bottom + 6}px;max-height:${Math.min(512, below - 6)}px`
				: `bottom:${window.innerHeight - rect.top + 6}px;max-height:${Math.min(512, above - 6)}px`;
		floating = `position:fixed;${open};left:${left}px;right:auto;width:${width}px`;
	}

	function close() {
		isOpen = false;
		floating = "";
	}

	function replace() {
		if (isOpen && floating) place();
	}

	function outsideScroll(event: Event) {
		if (isOpen && floating && !popover.contains(event.target as Node)) close();
	}

	async function toggle() {
		if (isOpen) {
			close();
			return;
		}
		place();
		isOpen = true;
		error = "";
		await tick();
		search?.focus();
	}

	function choose(token: string) {
		const option = parseToken(token);
		if (!option) {
			error = `Invalid picker token: ${token}`;
			return;
		}
		if (mode === "multiple") {
			selected = selected.includes(option.name)
				? selected.filter((item) => item !== option.name)
				: [...selected, option.name];
			return;
		}
		const result = qualifiedToken(token);
		if (targetId) {
			const target = document.getElementById(targetId);
			if (!(target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement)) {
				error = "The icon insertion field is unavailable.";
				return;
			}
			if (target.disabled || target.readOnly) {
				error = "The icon insertion field is not editable.";
				return;
			}
			const start = target.selectionStart ?? target.value.length;
			const end = target.selectionEnd ?? start;
			target.setRangeText(result, start, end, "end");
			target.dispatchEvent(new Event("input", { bubbles: true }));
			target.focus();
		} else {
			value = result;
			trigger.focus();
		}
		close();
		query = "";
	}

	function chooseQuality(next: string) {
		quality = next;
		if (mode === "single" && current && current.type !== "fluid") {
			// Explicit normal quality must remain distinct from "any" in filters.
			value = formatToken({ ...current, quality: next }, { keepNormal: allowAny });
		}
	}

	function keydown(event: KeyboardEvent) {
		if (event.key === "Escape" && isOpen) {
			event.preventDefault();
			close();
			trigger.focus();
		}
	}

	onMount(() => {
		const initialValue = value;
		const initialSelected = [...selected];
		const initialQuality = quality;
		const form = root.closest("form");
		function reset() {
			value = initialValue;
			selected = [...initialSelected];
			quality = initialQuality;
			query = "";
			close();
		}
		const unregister = registerDropdown({
			contains: (node) => root.contains(node),
			close,
		});
		form?.addEventListener("reset", reset);
		window.addEventListener("scroll", outsideScroll, true);
		window.addEventListener("resize", replace);
		return () => {
			unregister();
			form?.removeEventListener("reset", reset);
			window.removeEventListener("scroll", outsideScroll, true);
			window.removeEventListener("resize", replace);
		};
	});
</script>

<div
	class="icon-picker"
	class:icon-picker-filter={mode === "multiple"}
	class:icon-picker-insert={mode === "insert"}
	bind:this={root}
>
	{#if mode === "multiple"}
		<span class="field-label" id={`${id}-label`}>{label}</span>
		{#each selected as selection (selection)}
			<input type="hidden" {name} value={selection} />
		{/each}
	{:else if mode === "single"}
		<input type="hidden" {name} {value} />
	{/if}
	<button
		bind:this={trigger}
		type="button"
		class="icon-picker-trigger secondary outline"
		class:icon-button={mode === "insert"}
		class:icon-picker-input={mode === "single"}
		aria-label={label}
		aria-haspopup="dialog"
		aria-expanded={isOpen}
		aria-controls={`${id}-popover`}
		title={mode === "multiple" && selected.length
			? selected.join(", ")
			: current
				? value
				: label}
		onclick={toggle}
		onkeydown={keydown}
	>
		{#if mode === "multiple"}
			<span class="selected-value">{selected.length ? selected.join(", ") : placeholder}</span
			>
		{:else if mode === "insert"}
			<ImagePlus size={20} />
		{:else if selectedStyle}
			<span class="icon factorio-icon" style={selectedStyle} aria-hidden="true"></span>
		{:else}
			<span class="icon-placeholder" aria-hidden="true"></span>
		{/if}
	</button>
	<div
		bind:this={popover}
		id={`${id}-popover`}
		class="icon-picker-popover"
		style={floating || undefined}
		role="dialog"
		aria-label={label}
		hidden={!isOpen}
		tabindex="-1"
		onkeydown={keydown}
	>
		<input
			bind:this={search}
			type="search"
			placeholder="Filter icons..."
			aria-label={`Filter ${label.toLowerCase()}`}
			bind:value={query}
		/>
		<IconGrid {id} {kind} {query} {quality} {multiple} {selected} {current} onchoose={choose} />
		<QualityPicker
			{qualities}
			{quality}
			name={qualityName}
			disabled={mode === "single" && current?.type === "fluid"}
			onselect={chooseQuality}
		/>
		{#if error}<p role="alert">{error}</p>{/if}
	</div>
</div>

<style>
	.icon-picker {
		position: relative;
		min-width: 0;
	}
	.icon-picker-filter {
		display: grid;
		gap: 0.5rem;
	}
	.icon-picker-trigger {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 0.35rem;
		width: auto;
		margin: 0;
		padding: 0.35rem 0.65rem;
		font-size: 0.875rem;
	}
	.icon-picker-filter > .icon-picker-trigger {
		justify-content: space-between;
		width: 100%;
		min-height: 3rem;
		padding: var(--pico-form-element-spacing-vertical)
			var(--pico-form-element-spacing-horizontal);
		font-size: 1rem;
		border-color: var(--pico-form-element-border-color);
		background: var(--pico-form-element-background-color);
		color: var(--pico-color);
	}
	.icon-picker-filter > .icon-picker-trigger::after {
		flex-shrink: 0;
		content: "";
		width: 0.5rem;
		height: 0.5rem;
		border-right: 1px solid currentColor;
		border-bottom: 1px solid currentColor;
		transform: rotate(45deg);
	}
	/* As tall as the app's text inputs and selects, so it lines up beside them. */
	.icon-picker-input {
		width: 3.25rem;
		height: calc(
			1rem * var(--pico-line-height) + var(--pico-form-element-spacing-vertical) * 2 +
				var(--pico-border-width) * 2
		);
		padding: 0;
		border-color: var(--pico-form-element-border-color);
		background: var(--pico-form-element-background-color);
	}
	.icon-placeholder {
		width: 1.4rem;
		height: 1.4rem;
		border: 1px dashed var(--pico-muted-color);
		border-radius: 0.2rem;
	}
	.icon-picker-popover {
		position: absolute;
		z-index: 200;
		top: calc(100% + 0.4rem);
		left: 0;
		width: min(34rem, calc(100vw - 2rem));
		max-height: min(32rem, 75vh);
		overflow: auto;
		padding: 0.75rem;
		border: 1px solid var(--pico-muted-border-color);
		border-radius: var(--pico-border-radius);
		background: var(--pico-card-background-color, var(--pico-background-color));
		box-shadow: 0 0.5rem 1.5rem rgb(0 0 0 / 18%);
	}
	.icon-picker-popover[hidden] {
		display: none;
	}
	.icon-picker-insert .icon-picker-popover {
		left: auto;
		right: 0;
	}
	.icon-picker-popover > input {
		margin-bottom: 0.65rem;
	}
	.icon {
		width: 1.4rem;
		height: 1.4rem;
		font-size: 1.4rem;
	}
	.selected-value {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
</style>
