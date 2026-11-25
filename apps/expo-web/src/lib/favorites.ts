// Browser-only favorite toggling, shared by the favorite buttons and the
// account favorites list. The viewer's favorite state is server-rendered.
import { browserApi } from "@lib/browser-action";

export async function setFavorite(entryId: string, favorited: boolean) {
	const api = browserApi();
	if (favorited) await api.favorites.add(entryId);
	else await api.favorites.remove(entryId);
}
