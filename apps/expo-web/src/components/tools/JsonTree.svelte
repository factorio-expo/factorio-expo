<script lang="ts">
	// Read-only tree of the JSON textarea `sourceId` (string-tool.ts fires an
	// `input` event after it fills the field). Invalid JSON keeps the last good
	// tree and says so; the encode button reports the parse error itself.
	// Blueprints run to tens of thousands of nodes, so the open tree is flattened
	// into rows and only the rows in view are rendered. Colours come from Pico's
	// variables, so the tree follows the theme with no script.
	import { untrack } from "svelte";

	let { sourceId, json = "" }: { sourceId: string; json?: string } = $props();

	type Parsed = { data: object | undefined; valid: boolean };
	type Row = {
		path: string;
		depth: number;
		key: string | undefined;
		value: unknown;
		count: number;
		open: boolean;
	};

	const OVERSCAN = 8;
	// Cap on the `title` tooltip that shows a truncated string in full.
	const TITLE_MAX = 400;
	const SEPARATOR = "\u0001";
	// The depth opened at first: the root and its keys.
	const DEFAULT_DEPTH = 2;

	function parse(text: string): Parsed {
		if (!text.trim()) return { data: undefined, valid: true };
		try {
			const value: unknown = JSON.parse(text);
			return typeof value === "object" && value !== null
				? { data: value, valid: true }
				: { data: undefined, valid: false };
		} catch {
			return { data: undefined, valid: false };
		}
	}

	function childKeys(value: object): string[] {
		return Array.isArray(value) ? Array.from(value.keys(), String) : Object.keys(value);
	}

	// Raw state: the parsed document is never proxied.
	let tree = $state.raw<Parsed>(untrack(() => parse(json)));
	// Nodes open by default are those above `openDepth`; `toggled` flips single nodes.
	let openDepth = $state(DEFAULT_DEPTH);
	let toggled = $state.raw(new Set<string>());
	let active = $state(0);
	let scrollTop = $state(0);
	let viewHeight = $state(400);
	// 1.6rem rows; measured from the root font size once mounted.
	let rowHeight = $state(25.6);
	let scroller = $state<HTMLDivElement>();
	const uid = $props.id();

	const rows = $derived.by(() => {
		const out: Row[] = [];
		if (!tree.data) return out;
		const walk = (value: unknown, key: string | undefined, depth: number, path: string) => {
			const container = typeof value === "object" && value !== null;
			const keys = container ? childKeys(value) : [];
			const open = keys.length > 0 && depth < openDepth !== toggled.has(path);
			out.push({ path, depth, key, value, count: keys.length, open });
			if (!open) return;
			for (const k of keys)
				walk((value as Record<string, unknown>)[k], k, depth + 1, path + SEPARATOR + k);
		};
		walk(tree.data, undefined, 0, "");
		return out;
	});

	const first = $derived(Math.max(0, Math.floor(scrollTop / rowHeight) - OVERSCAN));
	const last = $derived(
		Math.min(rows.length, Math.ceil((scrollTop + viewHeight) / rowHeight) + OVERSCAN),
	);
	const visible = $derived(rows.slice(first, last));
	const current = $derived(Math.min(active, Math.max(rows.length - 1, 0)));

	function toggle(row: Row) {
		if (row.count === 0) return;
		const next = new Set(toggled);
		if (!next.delete(row.path)) next.add(row.path);
		toggled = next;
	}

	function setAll(depth: number) {
		openDepth = depth;
		toggled = new Set();
		active = 0;
		if (scroller) scroller.scrollTop = 0;
	}

	function reveal(index: number) {
		if (!scroller) return;
		const top = index * rowHeight;
		if (top < scroller.scrollTop) scroller.scrollTop = top;
		else if (top + rowHeight > scroller.scrollTop + scroller.clientHeight)
			scroller.scrollTop = top + rowHeight - scroller.clientHeight;
	}

	function go(index: number) {
		active = Math.min(Math.max(index, 0), rows.length - 1);
		reveal(active);
	}

	function onKeydown(event: KeyboardEvent) {
		const row = rows[current];
		if (!row) return;
		const page = Math.max(1, Math.floor(viewHeight / rowHeight) - 1);
		switch (event.key) {
			case "ArrowDown":
				go(current + 1);
				break;
			case "ArrowUp":
				go(current - 1);
				break;
			case "PageDown":
				go(current + page);
				break;
			case "PageUp":
				go(current - page);
				break;
			case "Home":
				go(0);
				break;
			case "End":
				go(rows.length - 1);
				break;
			case "ArrowRight":
				if (row.count > 0 && !row.open) toggle(row);
				else if (row.open) go(current + 1);
				break;
			case "ArrowLeft":
				if (row.open) toggle(row);
				else {
					let parent = current - 1;
					while (parent > 0 && rows[parent].depth >= row.depth) parent--;
					if (row.depth > 0) go(parent);
				}
				break;
			case "Enter":
			case " ":
				toggle(row);
				break;
			default:
				return;
		}
		event.preventDefault();
	}

	function onPointer(index: number) {
		active = first + index;
		toggle(rows[active]);
		scroller?.focus({ preventScroll: true });
	}

	function display(value: unknown): { text: string; kind: string } {
		if (value === null) return { text: "null", kind: "null" };
		if (typeof value === "string") return { text: JSON.stringify(value), kind: "string" };
		return { text: String(value), kind: typeof value === "boolean" ? "boolean" : "number" };
	}

	// A shorter tree clamps the browser's scroll position without always
	// firing a scroll event.
	$effect(() => {
		void rows.length;
		if (scroller) scrollTop = scroller.scrollTop;
	});

	$effect(() => {
		rowHeight = parseFloat(getComputedStyle(document.documentElement).fontSize) * 1.6;
	});

	// Typing in a large document re-parses it, so wait for a pause.
	$effect(() => {
		const source = document.getElementById(sourceId) as HTMLTextAreaElement | null;
		let timer: ReturnType<typeof setTimeout> | undefined;
		const update = () => {
			const next = parse(source?.value ?? "");
			// Keep showing the last good tree while the user is mid-edit.
			tree = next.valid ? next : { data: tree.data, valid: false };
		};
		const onInput = () => {
			clearTimeout(timer);
			timer = setTimeout(update, 200);
		};
		source?.addEventListener("input", onInput);
		return () => {
			clearTimeout(timer);
			source?.removeEventListener("input", onInput);
		};
	});
</script>

<div class="json-tree">
	{#if tree.data}
		<div class="json-tree-bar">
			<button type="button" class="json-tree-action" onclick={() => setAll(Infinity)}
				>Expand all</button
			>
			<button type="button" class="json-tree-action" onclick={() => setAll(1)}
				>Collapse all</button
			>
			{#if !tree.valid}<small>The JSON no longer parses; showing the last valid tree.</small
				>{/if}
		</div>
		<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
		<div
			class="json-tree-scroll mono"
			role="tree"
			tabindex="0"
			aria-label="Blueprint JSON tree"
			aria-activedescendant="{uid}-{current}"
			bind:this={scroller}
			bind:clientHeight={viewHeight}
			onscroll={() => (scrollTop = scroller!.scrollTop)}
			onkeydown={onKeydown}
		>
			<div
				class="json-tree-rows"
				style:height="{rows.length * rowHeight}px"
				style:padding-top="{first * rowHeight}px"
			>
				{#each visible as row, i (row.path)}
					{@const index = first + i}
					<!-- svelte-ignore a11y_click_events_have_key_events -->
					<div
						id="{uid}-{index}"
						class="json-tree-row"
						class:active={index === current}
						role="treeitem"
						tabindex="-1"
						aria-level={row.depth + 1}
						aria-selected={index === current}
						aria-expanded={row.count > 0 ? row.open : undefined}
						style:padding-inline-start="{row.depth * 1.2}rem"
						onclick={() => onPointer(i)}
					>
						<span class="json-tree-twist" aria-hidden="true"
							>{row.count > 0 ? (row.open ? "▾" : "▸") : ""}</span
						>
						{#if row.key !== undefined}<span class="json-tree-key">{row.key}</span><span
								class="json-tree-punct"
								>:
							</span>{/if}
						{#if typeof row.value === "object" && row.value !== null}
							<span class="json-tree-punct"
								>{Array.isArray(row.value)
									? `[${row.count}]`
									: `{${row.count}}`}</span
							>
						{:else}
							{@const shown = display(row.value)}
							<span
								class="json-tree-{shown.kind}"
								title={shown.text.length > 80
									? shown.text.slice(0, TITLE_MAX)
									: undefined}>{shown.text}</span
							>
						{/if}
					</div>
				{/each}
			</div>
		</div>
	{:else}
		<p class="json-tree-note">
			<small
				>{tree.valid
					? "Decode a string or enter JSON to see its tree."
					: "The JSON is not an object or array, or does not parse."}</small
			>
		</p>
	{/if}
</div>

<style>
	.json-tree {
		display: flex;
		flex-direction: column;
		height: 100%;
	}
	.json-tree-bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.25rem 0.5rem;
		padding: 0.25rem 0.5rem;
		border-bottom: var(--pico-border-width) solid var(--pico-muted-border-color);
	}
	.json-tree-action {
		width: auto;
		margin: 0;
		padding: 0.1rem 0.5rem;
		border-color: var(--pico-muted-border-color);
		background: transparent;
		color: var(--pico-muted-color);
		font-size: 0.75rem;
	}
	.json-tree-scroll {
		flex: 1;
		min-height: 0;
		overflow: auto;
		font-size: 0.8rem;
	}
	.json-tree-scroll:focus-visible {
		outline: 2px solid var(--pico-primary);
		outline-offset: -2px;
	}
	.json-tree-rows {
		box-sizing: border-box;
		min-width: max-content;
	}
	.json-tree-row {
		height: 1.6rem;
		line-height: 1.6rem;
		white-space: nowrap;
		cursor: default;
	}
	.json-tree-row.active {
		background: var(--pico-secondary-focus);
	}
	.json-tree-twist {
		display: inline-block;
		width: 1.1rem;
		text-align: center;
		color: var(--pico-muted-color);
	}
	.json-tree-key {
		color: var(--pico-color);
		font-weight: 600;
	}
	.json-tree-punct {
		color: var(--pico-muted-color);
	}
	.json-tree-string {
		color: var(--pico-ins-color);
	}
	.json-tree-number {
		color: var(--pico-primary);
	}
	.json-tree-boolean,
	.json-tree-null {
		color: var(--pico-del-color);
	}
	.json-tree-note {
		margin: 0;
		padding: 0.5rem;
	}
</style>
