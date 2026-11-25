<script lang="ts">
	// The challenge editor's goal and restrictions (challenge.Rules goal and
	// restrictions), written into one hidden `rules_json` field. A goal or
	// restriction on an item or fluid picks it with quality; a numeric one is
	// the parameter alone. Entities and recipes have their own rows, like the
	// site's production filters: entity or recipe, quality, comparison, count.
	import { untrack } from "svelte";
	import FilterRows from "@factorio-expo/ui/forms/FilterRows.svelte";
	import IconPicker from "@factorio-expo/ui/game/icon-picker/IconPicker.svelte";
	import { formatToken, hasQuality, parseToken } from "@factorio-expo/ui/game/icon-token.ts";
	import type {
		ChallengeParam,
		ChallengeRestriction,
		ChallengeRules,
		ChallengeTarget,
	} from "@lib/api/types";
	import { OPERATORS, ruleParams } from "@lib/challenge-rules";

	let {
		params,
		rules,
	}: { params: ChallengeParam[]; rules: Pick<ChallengeRules, "goal" | "restrictions"> } =
		$props();

	const { itemParams, numericParams } = ruleParams(params);
	const paramOf = (name: string) => params.find((param) => param.name === name);

	/** The picker token of a target ("" for none); no quality means any. */
	function tokenOf(target: ChallengeTarget, kind: "material" | "entity" | "recipe"): string {
		if (!target.name) return "";
		const type = kind === "material" ? (target.type === "fluid" ? "fluid" : "item") : kind;
		if (!target.quality || type === "fluid") return `[${type}=${target.name}]`;
		return formatToken(
			{ type, name: target.name, quality: target.quality },
			{ keepNormal: true },
		);
	}

	/** A picker token back to a target. */
	function targetOf(token: string): ChallengeTarget {
		const parsed = parseToken(token);
		if (!parsed) return {};
		return {
			type: parsed.type === "fluid" ? "fluid" : parsed.type === "item" ? "item" : undefined,
			name: parsed.name,
			quality: hasQuality(token) && parsed.type !== "fluid" ? parsed.quality : undefined,
		};
	}

	type Row = {
		param: string;
		token: string;
		op: ChallengeRestriction["op"];
		value: number | undefined;
	};
	type CountRow = {
		kind: "entity" | "recipe";
		token: string;
		op: ChallengeRestriction["op"];
		value: number | undefined;
	};

	const counted = (r: ChallengeRestriction) =>
		(r.param === "entity_count" && Boolean(r.target.name)) || r.param === "recipe_count";

	let goalParam = $state(untrack(() => rules.goal.param));
	let goalToken = $state(untrack(() => tokenOf(rules.goal.target, "material")));
	let direction = $state(untrack(() => rules.goal.direction));
	let restrictions = $state<Row[]>(
		untrack(() =>
			rules.restrictions
				.filter((r) => !counted(r))
				.map((r) => ({
					param: r.param,
					token: tokenOf(r.target, "material"),
					op: r.op,
					value: r.value,
				})),
		),
	);
	let counts = $state<CountRow[]>(
		untrack(() =>
			rules.restrictions.filter(counted).map((r) => {
				const kind = r.param === "recipe_count" ? "recipe" : "entity";
				return { kind, token: tokenOf(r.target, kind), op: r.op, value: r.value };
			}),
		),
	);

	const isItemParam = (name: string) => paramOf(name)?.target === "material";

	// Both row lists share their columns, so comparisons and values line up:
	// the parameter (or entity/recipe), its picker, the comparison, the value.
	const COLUMNS = "minmax(8rem, 22rem) 3.25rem 5rem minmax(6rem, 10rem)";

	let serialized = $derived(
		JSON.stringify({
			goal: {
				param: goalParam,
				target: isItemParam(goalParam) ? targetOf(goalToken) : {},
				direction,
			},
			restrictions: [
				...restrictions
					.filter((row) => row.param && row.value !== undefined && row.value !== null)
					.map((row) => ({
						param: row.param,
						target: isItemParam(row.param) ? targetOf(row.token) : {},
						op: row.op,
						value: row.value,
					})),
				...counts
					.filter((row) => row.token && row.value !== undefined && row.value !== null)
					.map((row) => {
						const target = targetOf(row.token);
						return {
							param: row.kind === "recipe" ? "recipe_count" : "entity_count",
							target: { name: target.name, quality: target.quality },
							op: row.op,
							value: row.value,
						};
					}),
			],
		}),
	);
</script>

<fieldset class="goal">
	<legend>Goal</legend>
	<div class="goal-row">
		<select aria-label="Goal parameter" bind:value={goalParam}>
			<optgroup label="Of an item or fluid">
				{#each itemParams as param (param.name)}
					<option value={param.name}>{param.label} ({param.unit})</option>
				{/each}
			</optgroup>
			<optgroup label="Numbers">
				{#each numericParams as param (param.name)}
					<option value={param.name}
						>{param.label}{param.unit ? ` (${param.unit})` : ""}</option
					>
				{/each}
			</optgroup>
		</select>
		{#if isItemParam(goalParam)}
			<IconPicker
				kind={["item", "fluid"]}
				allowAny
				bind:value={goalToken}
				label="Choose the goal's item or fluid"
			/>
		{/if}
		<select aria-label="Which wins" bind:value={direction}>
			<option value="max">Higher wins</option>
			<option value="min">Lower wins</option>
		</select>
	</div>
</fieldset>

<FilterRows
	bind:rows={restrictions}
	legend="Restrictions"
	noun="restriction"
	empty="No restrictions"
	max={20}
	columns={COLUMNS}
	newRow={() => ({ param: "footprint_area", token: "", op: "<=", value: undefined })}
>
	{#snippet row(item, index)}
		<select aria-label={`Restriction ${index + 1} parameter`} bind:value={item.param}>
			<optgroup label="Of an item or fluid">
				{#each itemParams as param (param.name)}
					<option value={param.name}>{param.label} ({param.unit})</option>
				{/each}
			</optgroup>
			<optgroup label="Numbers">
				{#each numericParams as param (param.name)}
					<option value={param.name}
						>{param.label}{param.unit ? ` (${param.unit})` : ""}</option
					>
				{/each}
			</optgroup>
		</select>
		<div>
			{#if isItemParam(item.param)}
				<IconPicker
					kind={["item", "fluid"]}
					allowAny
					bind:value={item.token}
					label={`Choose restriction ${index + 1}'s item or fluid`}
				/>
			{/if}
		</div>
		<select aria-label={`Restriction ${index + 1} comparison`} bind:value={item.op}>
			{#each OPERATORS as op (op)}
				<option value={op}>{op}</option>
			{/each}
		</select>
		<input
			type="number"
			step="any"
			placeholder="Value"
			aria-label={`Restriction ${index + 1} value`}
			bind:value={item.value}
		/>
	{/snippet}
</FilterRows>

<FilterRows
	bind:rows={counts}
	legend="Entities and recipes"
	hint="(how many entities, or entities set to the recipe)"
	noun="entity or recipe count"
	empty="No entity or recipe restrictions"
	max={30}
	columns={COLUMNS}
	newRow={() => ({ kind: "entity", token: "", op: "=", value: 0 })}
>
	{#snippet row(item, index)}
		<select
			aria-label={`Count ${index + 1} kind`}
			bind:value={item.kind}
			onchange={() => (item.token = "")}
		>
			<option value="entity">Entity</option>
			<option value="recipe">Recipe</option>
		</select>
		<div>
			{#key item.kind}
				<IconPicker
					kind={item.kind}
					allowAny
					bind:value={item.token}
					label={`Choose count ${index + 1}'s ${item.kind}`}
				/>
			{/key}
		</div>
		<select aria-label={`Count ${index + 1} comparison`} bind:value={item.op}>
			{#each OPERATORS as op (op)}
				<option value={op}>{op}</option>
			{/each}
		</select>
		<input
			type="number"
			min="0"
			step="1"
			placeholder="Count"
			aria-label={`Count ${index + 1} value`}
			bind:value={item.value}
		/>
	{/snippet}
</FilterRows>

<input type="hidden" name="rules_json" value={serialized} />

<style>
	.goal-row {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
		align-items: center;
	}
	.goal-row > :global(*) {
		width: auto;
		margin-bottom: 0;
	}
	.goal-row > select:first-child {
		flex: 1 1 14rem;
		max-width: 24rem;
	}
	.goal {
		margin-bottom: var(--pico-spacing);
	}
	:global(.filter-rows + .filter-rows) {
		margin-top: 1.25rem;
	}
</style>
