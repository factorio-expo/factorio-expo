// The one-time hand-over of a new anonymous entry's edit link: the form that
// created the entry remembers its token for this tab, the entry page (on its
// clean, shareable URL) shows the edit link once and forgets it. Storage can be
// unavailable (private windows, blocked site data); the entry still opens, only
// without the banner. Browser-only.

const key = (entryId: string) => `expo:edit-link:${entryId}`;

/** Keeps a new entry's edit token for the next page of this tab. */
export function rememberEditLink(entryId: string, token: string) {
	try {
		sessionStorage.setItem(key(entryId), token);
	} catch {
		// No storage: the entry page shows no edit link.
	}
}

/** Returns the remembered edit token once, then forgets it. */
export function takeEditLink(entryId: string): string | undefined {
	try {
		const token = sessionStorage.getItem(key(entryId)) ?? undefined;
		sessionStorage.removeItem(key(entryId));
		return token;
	} catch {
		return undefined;
	}
}
