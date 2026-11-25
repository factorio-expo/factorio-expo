// Keep in step with QUALITIES in @lib/icon-token (the icon-tokens.json fixture checks it).
export const qualityOptions = [
	{ value: "normal", label: "Normal" },
	{ value: "uncommon", label: "Uncommon" },
	{ value: "rare", label: "Rare" },
	{ value: "epic", label: "Epic" },
	{ value: "legendary", label: "Legendary" },
] as const;

export function compareQuality(left: string, right: string): number {
	const leftIndex = qualityOptions.findIndex((quality) => quality.value === left);
	const rightIndex = qualityOptions.findIndex((quality) => quality.value === right);
	return (
		(leftIndex < 0 ? qualityOptions.length : leftIndex) -
			(rightIndex < 0 ? qualityOptions.length : rightIndex) || left.localeCompare(right)
	);
}
