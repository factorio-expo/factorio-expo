# Search

Typesense is a disposable, rebuildable projection of PostgreSQL. PostgreSQL stays
the source of truth. Typesense was chosen over `tsvector`/`pg_trgm` for
numeric-range and cross-entity faceting, and over Meilisearch for multi-collection
joins. Code: `apps/expo-api/internal/search/`.

## Sync

- Every write that changes an entry's searchable facts upserts its
  `entry_search_jobs` row in the same transaction (`catalog/change.go`;
  imports and final analysis failures through `preview.Catalog`). A queued
  `delete` is never overwritten by an index job, so a late import cannot
  resurrect a deleted entry.
- `search.Dispatcher` (on `internal/outbox`: lease, 5 retries from 5 s capped
  at 5 min, then `failed`) syncs a row: `catalog.Service.ListedEntry` -> pure
  `projectEntry` -> Typesense. An entry that is no longer listed (private,
  unlisted, deleted, expired, quick preview) is deleted from the index. Each
  resolution is guarded by the claimed row's `updated_at`, so a newer intent
  queued mid-sync is kept.
- **Versioned collection:** `indexVersion()` hashes the schema, the community
  names and `projectionVersion`; the physical collection is `blueprints_<v>`,
  and the alias `blueprints` points at the published one.
- **`search.Keeper`** owns the index; the API never waits for it. At startup
  (`EnsureIndex`) it rebuilds unless the alias already points at this
  version: build, set the alias, switch searches, drop other versions. Until
  then searches read the previously published version. The dispatcher and
  the view-count refresh run only while the index is live. Hourly, a missing
  alias or a document count that differs from `CountSearchableDocuments`
  (compared only with no outstanding search job) triggers a rebuild. Entries
  that fail during a rebuild are queued for the dispatcher; a rebuild that
  fails whole is retried with backoff.
- **View counts** go as partial updates every `API_SEARCH_VIEW_INTERVAL`
  (`refreshViews`), never as search jobs.
- Editing `schema.go` means one automatic full rebuild on the next deploy.
  **Bump `projectionVersion`** when documents change without a schema change.
  **Force a rebuild** by deleting the `blueprints` alias.
- Replicas never sync the same row (`SKIP LOCKED`), but rebuilds are not
  coordinated between replicas.

## Collections

- `blueprints`: what `/api/v1/search` queries. One document per search result, its `kind` saying which (a facet, so a query can count each kind):
  - **`blueprint`:** each blueprint of the entry's current revision (id = the blueprint id), with its own facts: `search_text` (its label and description, icon tokens, `[color]`/`[font]` tags and `[space-age]` stripped), `icons`, `mods` (its own [mod detection](architecture.md#mod-detection): `vanilla` when it needs nothing beyond base, else `space-age` for any built-in match (space-age, quality, recycler, elevated-rails) and the names of other matched modsets; one with only unknown names, or none detected yet, gets none), `entity_names`, `recipes`, `entity_facts`/`recipe_facts`/`qualities` (quality-aware facet values), `product_items`/`ingredient_items`, dynamic `rate_out_*`/`rate_in_*` floats, the dynamic per-blueprint ratios `rate_tile_out_*` (output rate over the footprint area, `width * height` tiles) and `rate_mw_out_*` (output rate over `consumption_watts` in MW; omitted when nothing is consumed), dynamic `cost_raw_*` floats (the build cost's raw resources, items and fluids by name), `electricity_net_watts`, `pollution_net_per_second`, `entity_count`, `footprint_short`/`footprint_long` (omitted unless it has a current analysis, so unanalysed blueprints never match a size filter), `has_preview`, `has_platform` with `weight_tons`/`thrust_mn`/`max_speed_km_s` (an analysed space platform; see [platforms.md](platforms.md)), `top_product` (the item or fluid with the highest output rate, none without a current analysis: what [similar blueprints](#similar-blueprints) share).
  - **`upgrade_planner`/`deconstruction_planner`:** each planner, inside books too (id = the planner id), with its text and icons only: planners are never analysed, so an analysis filter (production, entities, recipes, quality, footprint, platform, factories/platforms, mods) never matches one.
  - **`book`:** each top-level book (id `book:<entry id>`), with the entry's text and icons and its blueprints' facts together (as projected from the entry's facts, `entryFacts`): their rates summed, but no per-tile, per-MW or build cost fields (a book has no one footprint or consumption), so those filters match blueprints only. Sub-books are not results.
  - **Shared:** `node_id` (the blueprint's or planner's id, empty on a book), `position` (tree order; -1 for the book, so it lists before its nodes), and the entry's filter and sort fields, copied to every document so a query needs no join: `entry_id`, `tags`, `game_version`, `version_groups` (the release families of the game version, `versions.go`: `pre-2.0` for 0.x and 1.x, else `v<major>.<minor>`, plus `v2.1-compatible` for a 2.1 blueprint or an older one `blueprint.Compat21` found compatible; the game version filter and its counts both use them), `spoiler` (the entry's spoiler/NSFW flag), `leaf_count`, `view_count`, `created_at`, `updated_at`. Display-only fields (stored, never matched): `title`, `description`, `icon_string` (the card's text and kind icon) and `book_title`.
  - **Text scoping:** a result matches only its own text (`query_by=search_text`). A leaf matches its label and description, a planner its own, a book its entry title and description plus its root's label and description. A book's text never makes its leaves match: "red science" lists the leaves whose own text says so, and the book only with `kind=book`. When the entry is its one blueprint or planner, the entry's title and description join that node's `search_text` and are the card's title and description. A node inside a book carries the entry title as `book_title`, shown on the card (the header link to the book), never matched. Ranking is Typesense text relevance only.

Production-rate fields aggregate by item name and, for items, quality tier
(fluids have no quality, so they only get the aggregate field); item names use
Factorio internal names with underscore-safe substitution. Each direction has
its own prefix and wildcard field (`rate_out_`, `rate_in_`, `rate_tile_out_`,
`rate_mw_out_`, and `cost_raw_` for the build cost), so no name can collide
across them. Rates come from `blueprint_stats.production`, so they carry the
entry's pinned content set. A stale analysis adds none of them. Typesense
cannot filter on an expression, so the ratios are computed at index time, per
product. Sorting by them or by a build cost is not offered: a sort would name a
dynamic field per product, and results without it would need a missing-values
rule.

## Community names

Players name items differently from the game files ("red science" is
`automation-science-pack`). The hand-written list
(`apps/expo-game-data/communitynames/names.json`, about 50 items, each with its
names; the game files hold none of these, so it is edited by hand, not
generated) is embedded by expo-api and loaded into Typesense as the synonym set
`community-names` (`PUT /synonym_sets/community-names`, one multi-way synonym per
item: its names, the item's words and its internal name) whenever the
collections are created (`createCollections`), and the blueprints collection
references it (`synonym_sets`). A search for "red science" therefore finds what
"automation science pack" and "automation-science-pack" find, and the other
way round. The list is part of `indexVersion()`, so editing it rebuilds the
index and reloads the set; the index stays a disposable projection. A test
checks that every item is a recipe of the pinned game
(`TestCommunityNamesPointAtRecipes`). Names are text only: an icon token or a
filter never uses them.

## Query (`GET /api/v1/search`)

`search.ParseQuery` reads every parameter through one filter table
(`internal/search/filters.go`): each row names its parameters, parses and
validates them (the only validation; an invalid one is a 400
`invalid_search`), names the schema fields it compares and builds its
`filter_by` clauses. `buildFilter`, `search.Parameters` (which the OpenAPI
test checks against `public.json`) and `TestFilterFieldsMatchSchemaAndProjection`
(every row's fields are declared in `schemaBlueprints` and written by the
projection) read the same table, so a new filter is one row. The web browse
page mirrors it with one descriptor per filter in
`apps/expo-web/src/lib/search/query.ts`.

| Param | Meaning |
|---|---|
| `q` | Free text plus icon tokens. `[fluid=water] cooling tower` requires the `[fluid=water]` icon (multiple tokens are ANDed) and text-matches the rest. A fluid token is matched without quality (`icontoken.Extract`), like the indexed icons. |
| `tags`, `entities`, `recipes` | Comma lists, OR within each facet. |
| `mods` | Comma list of `vanilla`, `space-age` and modset names (`GET /api/v1/modsets`), OR-matched: a blueprint matching any of them. Unknown values match nothing. |
| `entity_quality`, `recipe_quality` | `normal`..`legendary`; restrict entity/recipe filters to that tier. |
| `game_version` | Source game version from the blueprint: exact, or a release family (`version_groups`): `pre-2.0` (0.x and 1.x), `v2.0`, `v2.1`, `v2.1-compatible` -> made in 2.1 or found compatible ([2.1 compatibility](architecture.md#21-compatibility)). |
| `created_after`, `created_before` | The entry's `created_at` range, both ends included. Each is a date (`2026-11-14`: `created_after` from the start of that UTC day, `created_before` through its end) or an RFC 3339 timestamp (exact); anything else is a 400. The browse page has two date inputs (UTC days); either bound works alone. |
| `facets` | `true` also returns `facet_counts` (below). Off by default; the browse page asks. |
| `similar_to` | A blueprint's id: the [similar blueprints](#similar-blueprints) of it instead of the query's results; `sort` and `kind` are ignored (blueprints, closest rate first), `q` and the other filters apply. An id that is no live public blueprint is a 404. |
| `production_filters` | JSON array (at most 8) of `{type: item\|fluid, product, quality, direction: in\|out\|out_per_tile\|out_per_mw, operator: >\|=\|<, rate}`; ANDed, on the one blueprint's rates. `out_per_tile` compares the output rate per tile of footprint (`rate_tile_out_*`), `out_per_mw` the output rate per MW consumed (`rate_mw_out_*`); both match blueprints only. `type` is required, and a fluid filter with a quality is a 400 (fluids have none). Ignored for `blueprint_type=platforms`. |
| `cost_filters` | JSON array (at most 8) of `{type: item\|fluid, resource, operator: >\|=\|<, amount}`: the blueprint's build cost in that raw resource (`cost_raw_*`), ANDed; blueprints only. A resource no document has matches nothing. |
| `spoiler` | `exclude` leaves out entries marked spoiler/NSFW, `only` keeps only them; absent keeps every entry. Anything else is a 400. The web browse page sends nothing (every entry) unless "Hide spoiler/NSFW entries" is ticked (`?spoiler=hide` in its URL; the API then gets `exclude`). |
| `kind` | Comma list of `blueprint`, `book`, `upgrade_planner`, `deconstruction_planner` (the result's kind), OR-matched; none means every kind, so a book and its own blueprints and planners can all be results (the web browse page sends `blueprint` unless the visitor picks kinds). Anything else is a 400. |
| `footprint_width`, `footprint_height` | Tiles: the result fits in width x height either way round (`footprint_short <= min`, `footprint_long <= max`; a book's are its largest blueprint sides); one alone limits the short side. Results without a measured footprint (planners, unanalysed blueprints) never match. |
| `blueprint_type` | `all` (default), `factories` (not a space platform), `platforms` (space platforms only). |
| `platform_filters` | JSON array (at most 8) of `{metric: weight_tons\|thrust_mn\|max_speed_km_s, operator: >\|=\|<, value}`; implies a platform match. Ignored for `factories`. |
| `sort` | `relevance`, `created_at:asc\|desc`, `updated_at:asc\|desc`, `entity_count:asc\|desc` (the blueprint's), `leaf_count:asc\|desc` (its entry's), `view_count:asc\|desc` (its entry's). Default `updated_at:desc`. The leaf's `position` is the last tiebreak, so a book's leaves list in book order. Applied before pagination. |
| `limit`, `offset` | Default 20, max 100. Any offset works (sent to Typesense as `offset`/`limit`, not a page index). |

The response is a slim card list, one card per matching result (`total`
counts results): `entry_id`, `node_id` (absent on a book), `kind`,
`title`, `description` and `icons` (the node's own, or the entry's for a book
or an entry's only node), `book_title` (set when the node is inside a book:
the web card links the book's entry from its header, and opens a blueprint's
page or a planner's row on the entry page, `#node-<id>`),
`preview_url`/`preview_pending` (a blueprint's thumbnail from
`catalog.LeafPreviews`, a book's cover from its entry card; planners have
none), `spoiler` (read live from the entry card, like the owner), `game_version`, `tags`, `mod_badges` (the documents' stored `mod_badges`: `space-age`, then guessed modsets scoring at least 0.5, as `mods` filter values; a book's are its leaves' together; not filterable, use `mods`), `favorites_count`, `view_count`,
`is_favorited`, owner, timestamps, `records` (the leaderboard boards it leads now, read live from `board_leaders`, never indexed: [api.md](api.md#profiles-leaderboards-challenges)). Beside the cards, `kind_counts` gives the
matches per kind with the kind filter left out (a facet on `kind`: from the
same request when no kind is selected, else from a second facet-only request),
so the kind filter can show each option's count.

With `facets=true` the response also has `facet_counts` for the browse filter
panel, a small curated set (no counts on rate, cost or footprint filters): `tags`
(per tag slug), `platform` (`factories` and `platforms`, the `blueprint_type`
values), `mods` (per `mods` value: `vanilla`, `space-age`, modset names) and
`versions` (per `game_version` family). Counts are of results (leaves, books,
planners as the query has them), each with its own filter left out and every
other filter applied (`typesense_facets.go`: a facet whose filter is set is
counted by a facet-only request without it; the others share the main
request's `facet_by`). A value with no match is absent; the page shows 0. Favorites, owner and views
come from the shared entry-card query (`catalog.NodeCards` over
`catalog.EntryCards`, the same cards every listing, board and home pick uses)
after Typesense returns IDs, so favoriting never triggers
a reindex. Only a live, public entry gets a card, and only a blueprint of its
current revision: a hit without one is dropped. Right after a delete, expiry,
a new revision or a change to unlisted/private the index may still hold the
old documents, but search never shows them (`total` can briefly count them).
Matching-only facts stay in the index. Ranking is Typesense text relevance
only; recency/favorites blending is not done.

## Similar blueprints

`similar_to=<blueprint id>` (`similar.go`) reads the blueprint's document and
turns its analysed facts into filters: the same `top_product` (the output with
the highest rate), the same `has_platform` (ground factories compare with
ground factories, space platforms with platforms), `kind:=blueprint`, and
`id:!=` the blueprint itself. Only what is in the index qualifies, so only live
public blueprints of non-quick-preview entries; the seed must be one too, else
404. A seed without a current analysis or any output has no similar blueprints
(an empty result). The query's `q` and filters are ANDed.

Order is closeness of the production rate of that product, on a ratio scale:
`|ln(rate / seed rate)|`, so half and double the rate are equally far. Typesense
cannot sort by distance from a value, so two requests read the blueprints at
or above the seed's rate (rate ascending) and those below it (descending), each
the first `offset + limit`, and the nearest are merged; ties list the upper side
first, then newest. At most the 250 nearest can be paged to. `total` counts every
similar blueprint; `kind_counts` is `{blueprint: total}` and there are no
`facet_counts`. The leaf page shows the first 6 and links
`/browse?similar_to=` for the rest ([frontend.md](frontend.md#pages)).
