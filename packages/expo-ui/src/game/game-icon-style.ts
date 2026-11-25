export type GameIconCoordinates = readonly [number, number, string];

export function gameIconStyle(coordinates: GameIconCoordinates | undefined): string | undefined {
	if (!coordinates) return undefined;
	const [x, y, sheet] = coordinates;
	const position = `${x === 0 ? "0" : `-${x / 64}em`} ${y === 0 ? "0" : `-${y / 64}em`}`;
	return `--factorio-icon-position:${position};--factorio-icon-image:var(--factorio-${sheet}-image);--factorio-icon-columns:var(--factorio-${sheet}-columns);--factorio-icon-rows:var(--factorio-${sheet}-rows)`;
}

export function withQualityIcon(
	style: string | undefined,
	qualityStyle: string | undefined,
): string | undefined {
	if (!style || !qualityStyle) return style;
	return `${style};${qualityStyle.replaceAll("--factorio-icon", "--factorio-quality-icon")}`;
}
