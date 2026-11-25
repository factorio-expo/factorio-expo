// The OpenSearch 1.1 description document (pages/opensearch.xml.ts): the
// browse page's text search as a browser search engine.

/** The descriptor's path, linked from Page.astro's head. */
export const OPENSEARCH_PATH = "/opensearch.xml";

/** The description document for the site at `site`. */
export function openSearchDescription(site: URL | string): string {
	const url = (path: string) => new URL(path, site).toString();
	return [
		'<?xml version="1.0" encoding="UTF-8"?>',
		'<OpenSearchDescription xmlns="http://a9.com/-/spec/opensearch/1.1/" xmlns:moz="http://www.mozilla.org/2006/browser/search/">',
		"\t<ShortName>Factorio Expo</ShortName>",
		"\t<Description>Search Factorio blueprints on Factorio Expo</Description>",
		"\t<InputEncoding>UTF-8</InputEncoding>",
		`\t<Image width="16" height="16" type="image/x-icon">${url("/favicon.ico")}</Image>`,
		`\t<Url type="text/html" method="get" template="${url("/browse")}?q={searchTerms}"/>`,
		`\t<Url type="application/opensearchdescription+xml" rel="self" template="${url(OPENSEARCH_PATH)}"/>`,
		`\t<moz:SearchForm>${url("/browse")}</moz:SearchForm>`,
		"</OpenSearchDescription>",
		"",
	].join("\n");
}
