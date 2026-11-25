# API client

The API's Go code is its definition: its Huma operations generate an OpenAPI
document (`apps/expo-api/openapi/public.json`), and `packages/expo-api-types`
generates TypeScript types from it. The client calls those operations through
[openapi-fetch](https://openapi-ts.dev/openapi-fetch/), typed by the generated
`paths`: every public route is a Huma operation, so every call is typed.

- `http-client.ts`: `ApiClient` (base URL, bearer (SSR) / cookie (`credentials`) / edit-token auth, extra headers, timeout). `api` is the typed client: `ok(http.api.GET("/api/v1/tags"))` resolves to the response or throws `ApiError`. The edit token goes as the `X-Edit-Token` header on every request (operations that take none ignore it), so no call passes it. `ok`, `ApiError` and the timeout are shared with the staff console (`@factorio-expo/ui/api/problem.ts`).
- `resources/*.ts`: one typed function per endpoint, grouped like `internal/httpapi/routes.go` (auth, blueprints, challenges, collections, comments, entries, favorites, modsets, search, sitemap, tags, users).
- `asset-url.ts`: public URLs of API paths (preview images, raw strings) for links and `<img src>`.
- `types.ts`: the generated types re-exported under the site's names (`Entry`, `LeafDetail`, `EntryProgress`, ...), and hand-mirrored types of the routes not documented yet; update those by hand when their Go types change.
- `index.ts`: `createApiClient({ baseUrl, token?, editToken?, credentials?, headers?, timeoutMs? })`.
- `public-asset.ts`: `assetUrl(path)`, an API path as the public URL markup uses (`toPublicAssetUrl` on `PUBLIC_API_BASE_URL`).
- `server.ts` / `browser.ts`: pick the base URL and read the page URL's edit token (`lib/entry-links.ts` `editTokenOf`); `browser.ts` also has `entryEventsUrl`, the progress stream's URL. SSR uses `API_INTERNAL_BASE_URL` (compose hostname); browser uses `PUBLIC_API_BASE_URL`. Never import `astro:env/server` client-side or send internal URLs to the browser (`previewUrl` goes into `<img src>` and must be public).

```astro
---
import { getServerApiClient } from "../lib/api/server";
// Acts as the visitor: session cookie -> bearer token, visitor IP -> X-Real-IP.
const api = getServerApiClient(Astro);
const results = await api.search.query({ q: Astro.url.searchParams.get("q") ?? "" });
---
```

```ts
import { getBrowserApiClient } from "../lib/api/browser";
// The HttpOnly session cookie is sent automatically (credentials: "include").
const api = getBrowserApiClient();
await api.favorites.add(entryId);
```

Build a fresh client per SSR request. `ApiError` carries `status`, `statusText`,
the raw `body` and the API's problem+json read from it: `code` (branch on it, or
on `status`), `detail` (the text for people, with a `validation_failed`
problem's invalid fields appended) and `fields` (such as `failure_code`).
