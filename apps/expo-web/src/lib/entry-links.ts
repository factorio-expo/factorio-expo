// Links between an entry's pages, and the anonymous editor's context. An
// anonymous entry's edit token travels in the page URL (`?token=`): the API
// clients read it from there by themselves (lib/api/server.ts, browser.ts, as
// the X-Edit-Token header), and `editorLinks` binds it to every link from one
// of the entry's pages to another (the entry, an older revision, a leaf, the
// new-revision form), so the holder stays an editor. Nothing else spells out
// `token`; the one-time hand-over of a new entry's link is lib/edit-link.ts.

const TOKEN = "token";

/** The edit token a page URL carries, if any. */
export function editTokenOf(url: URL | string): string | undefined {
	const parsed = typeof url === "string" ? new URL(url) : url;
	return parsed.searchParams.get(TOKEN) || undefined;
}

function withQuery(path: string, query: Record<string, string | undefined>): string {
	const params = new URLSearchParams();
	for (const [name, value] of Object.entries(query)) if (value) params.set(name, value);
	return params.size ? `${path}?${params}` : path;
}

const entryBase = (entryId: string) => `/e/${encodeURIComponent(entryId)}`;

/** The entry page; `revision`: an older revision to show (the live one needs none). */
export function entryPath(entryId: string, revision?: string): string {
	return withQuery(entryBase(entryId), { revision });
}

/** One leaf's page; the leaf knows its own revision. */
export function leafPath(entryId: string, blueprintId: string): string {
	return `${entryBase(entryId)}/b/${encodeURIComponent(blueprintId)}`;
}

/** The fragment of a node's row in the entry page's book tree. */
export function nodeAnchor(nodeId: string): string {
	return `#node-${encodeURIComponent(nodeId)}`;
}

/** The links of an entry's pages as the viewer of `pageUrl` follows them. */
export interface EditorLinks {
	/** The edit token the page carries, if any. */
	readonly token: string | undefined;
	entry(entryId: string, revision?: string): string;
	leaf(entryId: string, blueprintId: string): string;
	/** The form that adds a revision. */
	newRevision(entryId: string): string;
}

/** Links that carry the edit token of the page at `pageUrl`, if it has one. */
export function editorLinks(pageUrl: URL | string): EditorLinks {
	const token = editTokenOf(pageUrl);
	const carry = (path: string) => {
		if (!token) return path;
		return `${path}${path.includes("?") ? "&" : "?"}${new URLSearchParams({ [TOKEN]: token })}`;
	};
	return {
		token,
		entry: (entryId, revision) => carry(entryPath(entryId, revision)),
		leaf: (entryId, blueprintId) => carry(leafPath(entryId, blueprintId)),
		newRevision: (entryId) => carry(`${entryBase(entryId)}/revisions/new`),
	};
}

/** The edit link handed to an anonymous uploader: the entry page with its token. */
export function editLinkUrl(entryId: string, token: string, origin: string): string {
	const page = new URL(entryPath(entryId), origin);
	page.searchParams.set(TOKEN, token);
	return page.toString();
}
