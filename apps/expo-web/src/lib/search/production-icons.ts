// Server-side icon styles for production and build cost filter rows, so the
// filter islands can show icons before they load the icon sprite tables.
import type { SearchCostFilter, SearchProductionFilter } from "@lib/api";
import { formatToken } from "@factorio-expo/ui/game/icon-token.ts";
import { gameIcon } from "@factorio-expo/ui/game/game-icons.ts";

export function productionFilterIcons(filters: SearchProductionFilter[]) {
	return filters.map(({ type, product, quality }) => {
		const style = gameIcon(type, product, quality);
		const token = formatToken(
			{ type, name: product, quality: quality ?? "" },
			{ keepNormal: true },
		);
		return { value: token, style };
	});
}

export function costFilterIcons(filters: SearchCostFilter[]) {
	return filters.map(({ type, resource }) => ({
		value: formatToken({ type, name: resource, quality: "" }),
		style: gameIcon(type, resource),
	}));
}
