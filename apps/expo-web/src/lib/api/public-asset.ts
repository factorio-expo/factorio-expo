// The public URL of an API asset path, for markup sent to a browser (SSR and
// browser code alike): asset-url.ts toPublicAssetUrl on PUBLIC_API_BASE_URL.
import { PUBLIC_API_BASE_URL } from "astro:env/client";

import { toPublicAssetUrl } from "./asset-url";

/** An API path (a preview, a thumbnail, a raw string) as a URL a browser can load. */
export const assetUrl = (path: string) => toPublicAssetUrl(PUBLIC_API_BASE_URL, path);
