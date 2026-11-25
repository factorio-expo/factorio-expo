// Pages whose body does not depend on the visitor, so a CDN may keep one
// copy at the edge. Only the shell differs per visitor (the header and sidebar
// show who is signed in), so the copy is for anonymous requests only: the
// middleware sends these headers when there is no session cookie, and a
// CDN cache rule bypasses the cache when the cookie is present.
const EDGE_CACHEABLE_PATHS = new Set(["/", "/about", "/privacy-policy", "/terms-of-use"]);

/**
 * Browsers always revalidate (max-age=0), so a visitor who signs in sees the
 * signed-in shell at once; the edge keeps a copy for 10 minutes and may serve
 * it stale for a day while it refreshes (a deploy renames the hashed assets).
 */
const EDGE_CACHE_CONTROL = "public, max-age=0, s-maxage=600, stale-while-revalidate=86400";

/** The Cache-Control for an anonymous request to `pathname`, if the page is edge-cacheable. */
export function anonymousCacheControl(pathname: string): string | undefined {
	return EDGE_CACHEABLE_PATHS.has(pathname) ? EDGE_CACHE_CONTROL : undefined;
}
