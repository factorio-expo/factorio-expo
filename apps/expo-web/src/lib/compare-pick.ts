// The blueprint picked for comparison, remembered per browser tab in
// sessionStorage: the Compare button on one leaf page stores its id, the
// button on another leaf page then opens /compare?a=&b=. Storage can be
// missing or throw (private windows), so every access is guarded and the
// button simply starts over.

const KEY = "expo:compare-leaf";

/** A blueprint id: 12 letters and digits. */
const isLeafId = (value: unknown): value is string =>
	typeof value === "string" && /^[0-9A-Za-z]{12}$/.test(value);

/** This tab's sessionStorage, or undefined where the browser refuses it. */
export function tabStorage(): Storage | undefined {
	try {
		return window.sessionStorage;
	} catch {
		return undefined;
	}
}

/** The picked blueprint's id, or undefined when none is (or storage is unavailable). */
export function pickedLeaf(storage: Pick<Storage, "getItem">): string | undefined {
	try {
		const value = storage.getItem(KEY);
		return isLeafId(value) ? value : undefined;
	} catch {
		return undefined;
	}
}

/** Remembers `id` as the picked blueprint, or forgets it with undefined. */
export function pickLeaf(storage: Pick<Storage, "setItem" | "removeItem">, id?: string): void {
	try {
		if (id === undefined) storage.removeItem(KEY);
		else storage.setItem(KEY, id);
	} catch {
		// Without storage nothing is remembered.
	}
}

/** What pressing Compare on the blueprint `current` does, given the picked one. */
export type CompareAction = { kind: "pick" } | { kind: "clear" } | { kind: "open"; href: string };

export function compareAction(picked: string | undefined, current: string): CompareAction {
	if (picked === undefined) return { kind: "pick" };
	if (picked === current) return { kind: "clear" };
	return {
		kind: "open",
		href: `/compare?a=${encodeURIComponent(picked)}&b=${encodeURIComponent(current)}`,
	};
}
