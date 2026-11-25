import { GAME_ICON_COORDINATES, GAME_ICON_SHEETS } from "./data/game-icons.generated";
import { gameIconStyle, withQualityIcon } from "./game-icon-style";

export { GAME_ICON_SHEETS };

export function gameIcon(
	prototypeType: string,
	name: string,
	quality = "normal",
): string | undefined {
	const style = gameIconStyle(GAME_ICON_COORDINATES[prototypeType]?.[name]);
	if (!style || quality === "normal") return style;
	return withQualityIcon(
		style,
		gameIconStyle(GAME_ICON_COORDINATES["quality-badge"]?.[`${quality}-badge`]),
	);
}

/**
 * The CSS custom properties that point each sprite sheet at its image under
 * `baseUrl` (root-relative or https, ending in "/"): put them on <html>.
 */
export function gameIconVariables(baseUrl: string): string {
	const base = baseUrl.endsWith("/") ? baseUrl : `${baseUrl}/`;
	if (!/^(?:\/|https:\/\/)[A-Za-z0-9\-._~:/?#[\]@!$&'*,;=%+]*$/.test(base)) {
		throw new Error("the game icons base URL must be a safe root-relative or HTTPS URL");
	}
	return Object.entries(GAME_ICON_SHEETS)
		.map(([sheet, grid]) => {
			const sheetImage = `url("${base}${sheet}.webp")`;
			return `--factorio-${sheet}-columns:${grid.columns};--factorio-${sheet}-rows:${grid.rows};--factorio-${sheet}-image:${sheetImage}`;
		})
		.join(";");
}
