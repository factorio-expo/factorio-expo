// What the viewer of an entry page may do, decided once per request (SSR):
// the entry page, a leaf page and the new-revision form all load the entry
// and the viewer's rights here instead of re-deriving them. Pure apart from
// the API client it is given, so it is unit tested with a fake.
import { ApiError, type ApiClientInstance, type Entry } from "@lib/api";

export interface EntryAccess {
	entry: Entry;
	/**
	 * The viewer created it: its owner, or the holder of its edit token. For a
	 * quick preview this means they may publish it; for an anonymous entry, also
	 * claim it.
	 */
	creator: boolean;
	/** May edit details, add revisions and delete: a creator, never on a quick preview. */
	canEdit: boolean;
	/** Holders of the edit link (not the owner) of an anonymous entry may claim it. */
	claimable: boolean;
}

type EntriesApi = Pick<ApiClientInstance["entries"], "get" | "permissions">;

/** The value of a request, or undefined when the API answers 404. */
export async function unlessMissing<T>(request: Promise<T>): Promise<T | undefined> {
	try {
		return await request;
	} catch (err) {
		if (err instanceof ApiError && err.status === 404) return undefined;
		throw err;
	}
}

/**
 * Loads the entry and the viewer's rights (both requests at once).
 * Undefined when the entry is missing or hidden from the viewer. A failed
 * permissions check means no rights, never a broken page.
 */
export async function loadEntryAccess(
	api: { entries: EntriesApi },
	entryId: string,
): Promise<EntryAccess | undefined> {
	const [entry, creator] = await Promise.all([
		unlessMissing(api.entries.get(entryId)),
		api.entries
			.permissions(entryId)
			.then((permissions) => permissions.can_edit)
			.catch(() => false),
	]);
	if (!entry) return undefined;
	const preview = entry.quick_preview === true;
	return {
		entry,
		creator,
		canEdit: creator && !preview,
		claimable: creator && !preview && !entry.owner_user_id,
	};
}
