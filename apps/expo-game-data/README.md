# Factorio game data

The one-off generator of everything Expo reads from the game itself: the game
icon sprite sheets, the icon picker lists and the mod name table for mod
detection. Run it with

```sh
docker compose --profile tools run --build --rm expo-game-data
```

after upgrading the pinned game or editing `modsets.json`, then rebuild
expo-api (it embeds the mod table) and expo-web (it bundles the icon table).

## What a run does

1. Reads the built-in mods from the game's `data/*/info.json` (base,
   elevated-rails, recycler, quality, space-age; core is the engine).
2. Reads `modsets.json` (mounted from this directory) and resolves each
   modset against the [mod portal](https://mods.factorio.com): for every mod
   the newest release made for the pinned game's major.minor whose built-in
   requirements the pinned game meets and that its dependents allow, with its
   required dependencies and optional (`?`, not hidden `(?)`) dependencies on
   built-in mods (so a mod's optional Space Age content is in its dump).
3. Downloads the portal mods into `/data/mod-cache/`, checked against the
   portal's sha1; a cached zip with the right checksum is never downloaded
   again. Downloads need `FACTORIO_USERNAME` and `FACTORIO_TOKEN` from `.env`
   (the same as `factorio-downloader`); the token never appears in logs.
4. Dumps prototype data (`--dump-data`) for each built-in mod in dependency
   order, then for each modset, plus each one's parent (the built-in mods it
   loads, minus its own). Dumps are kept in `/data/dumps/` named by their mod
   list fingerprint, so a run only dumps what changed.
5. Writes the mod name table `apps/expo-api/internal/mods/modsets.json`: per
   modset, the entity and recipe names it adds over its parent (base lists the
   whole base game; recycling recipes are left out, no blueprint sets one).
6. Draws the icon sheets and writes the icon picker lists from the vanilla dump
   (every built-in mod on).

Everything under `/data` lives in the `game-data` Compose volume. Drop a stale
dump or mod with
`docker compose --profile tools run --rm --no-deps --entrypoint rm expo-game-data -rf /data/dumps`.

## `communitynames/names.json`

Not generated: the names players use for items ("red science" for
`automation-science-pack`), written by hand because the game files hold none.
Each entry is an internal item name (it must be a recipe of the pinned game; a
test checks) and its names, lowercase words without hyphens. expo-api embeds the
list (its Dockerfile copies the `communitynames` package) and loads it into
Typesense as synonyms whenever the search collections are created; editing it
rebuilds the index on the next deploy ([search.md](../../docs/search.md#community-names)).
No generator run is needed.

## `modsets.json`

Hand-edited; only mod names, never versions or credentials:

```json
{
  "modsets": [
    { "name": "krastorio-2", "title": "Krastorio 2 Spaced Out", "mods": ["Krastorio2-spaced-out"] }
  ]
}
```

`name` is the modset's id (lowercase letters, digits, dashes; not a built-in
mod's name), `title` is shown on the site, and `mods` are portal mod names
(dependencies are added automatically). A mod may carry a version condition in
info.json syntax, such as `"muluna-graphics <= 1.1.18"`, for pairings a mod's
own scripts enforce but its portal metadata does not state. List only mods that add entities or
recipes worth naming on a blueprint page. A mod with no release for the pinned
game (for example one still on Factorio 2.0) stops the run with an error naming
it.

## Icon sheets

The sheets go directly into the shared package's `packages/expo-ui/game-icons/`
directory (both apps serve and ship them through its Astro integration); the
container writes four lossy WebP sprite sheets and the TypeScript lookup table
`packages/expo-ui/src/game/data/game-icons.generated.ts`, removing older sheet files
(`.png`, `.jxl`, `.json`, `.webp`) from the asset directory first. Modded icons
are not drawn: most mod graphics licences do not allow redistribution.

Sheets are split by how often a page needs them; the browser only fetches a
sheet once an icon on it is shown:

- `core`: items, entities, fluids, qualities and the recipes that share a name
  with one of them (one cell serves all). Nearly every page uses it.
- `recipes`: the remaining non-recycling recipes (oil processing, barrel
  filling, ...). Splitting them out keeps `core` about 18% smaller.
- `signals`: virtual signals, planets and space locations, plus the engine's
  rich-text utility sprites (the `[space-age]` logo, lookup type `utility`).
- `recycling`: recycling recipes, rarely shown.

Within a sheet, names are deduplicated (an item, its entity and its recipe
share one cell, using the icon with the most layers) and so are identical
pictures: a cell whose pixels match one already drawn, on this sheet or an
earlier one in the order above, points at that cell instead. That is how
`item-unknown`, `recipe-unknown`, `fluid-unknown` and `signal-unknown` share one
core cell, and recycling recipes that look like a core icon reuse it. Lookups
therefore carry their own sheet ID. Recipes without an explicit icon that fall
back to a fluid result reuse that fluid's cell, even when their names differ;
`parameter-0` through `parameter-9` have no fluid cells, their fluid lookups
(and recipes falling back to them) alias the item cells.

Quality icons live in `core`: full-size 64px icons under the `quality` lookup
type and transparent cells with a lower-left badge under `quality-badge` (names
such as `rare-badge`). Normal comes from `base/graphics/icons/quality-normal.png`;
uncommon, rare, epic, and legendary come from the quality module. The web app
overlays badges on prototype icons, leaving normal quality unmarked. The
`quality=any` lookup shares the `virtual-signal=signal-any-quality` cell.

Empty categories still
produce a valid 1x1 sheet. Grid columns are `ceil(sqrt(cell count))`; rows are
the minimum needed for those columns. Entity entries are aliases, not a
complete entity catalog: the data parser identifies candidate entities by
collision or selection box (excluding known non-entity prototype types), then
emits one only when the same name has an icon-bearing prototype whose type is
`item`, is a known item subtype, or has `stack_size`. The alias uses that
item's icon. `signal-unknown` is emitted only when Factorio data contains the
real virtual-signal prototype; it is not inserted as a synthetic cell. The
container does not need to run an HTTP server. After regenerating, rebuild the
web app so Astro bundles the coordinate table and copies the image assets to
its static output.

The generated table exports `GAME_ICON_SHEETS` with each sheet's grid
dimensions and `GAME_ICON_COORDINATES` with prototype/name coordinates as an
`[x, y, sheetId]` tuple.

## Icon picker lists

`packages/expo-ui/src/game/data/icon-picker.data.ts` (git-ignored, like the icon
table) holds the shared icon picker's lists, laid out like the game's inventory
and the [blueprint editor](https://github.com/FactoryGameFan/factorio-blueprint-editor)'s
pickers: one tab per item group, one row per subgroup, each level sorted by
its `order` string and then by name. The `environment`, `enemies`, `effects`,
`tiles` and `other` groups and hidden prototypes are left out. Kinds:

- `icon` (blueprint icons, rich text): items, fluids (default subgroup
  `fluid`), virtual signals, recipes with a subgroup of their own whose
  name no item or fluid already has (oil processing, casting, ...), and
  planets and space locations (the space tab's last row, subgroup `planets`).
- `item` (item filters): items.
- `fluid`: fluids. The production-rate filter offers `item` and `fluid`
  together; the web picker merges several kinds tab by tab.
- `entity` (entity search): each item's `place_result` entity, at the item's
  place, when the entity has the `player-creation` flag (what blueprints
  hold; seeds place plants, which lack it). Tiles are left out, search counts
  entities only. A last `creative` tab holds the hidden creative entities
  (loaders, infinity chest and pipe, heat and electric energy interfaces).
- `recipe`: recipes, at their own subgroup and order or, failing that, their
  main product's (`main_product`, or the only result), as the game does.

Items are prototypes with a `stack_size`; equipment shares its item's name but
is not one.

## Encoding

Sheets are encoded with `cwebp -q 82 -m 6 -sharp_yuv -alpha_q 100` (lossy
colour, lossless alpha). There is no fallback format: every browser in use
decodes WebP. Measured on the
merged 429-cell core sheet (Debian trixie: libwebp 1.5, libjxl 0.11, libavif
1.2), quality as SSIMULACRA2 over dark and light backgrounds:

| Encoding | Size | SSIMULACRA2 | Decode (1 thread) |
|---|---|---|---|
| WebP lossless (previous) | 2249 KiB | 100 | 37 ms |
| WebP near-lossless 40 | 1324 KiB | 92 | 32 ms |
| JPEG XL `-d 3 -e 11` (previous) | 1251 KiB | 82 | 206 ms |
| AVIF q50 4:4:4 | 948 KiB | 81 | - |
| JPEG XL `-d 1.5 -e 7` | 883 KiB | 83 | 184 ms |
| **WebP q82 `-sharp_yuv`** | **826 KiB** | **82** | **52 ms** |
| WebP q82 without `-sharp_yuv` | 795 KiB | 76 | 52 ms |

JPEG XL at effort 9 and above is about 1.5x larger than effort 7 at the same
distance on these sheets; at equal quality, effort-7 JPEG XL ties WebP in size
but decodes about 3.5x slower. AVIF is larger at equal quality. At q82 the
icons are indistinguishable from lossless at 2x zoom. Other cwebp flags
(`-pass`, `-alpha_filter`, `-sns`, `-af`) changed nothing.

Layout barely matters: 8 to 64 columns, hue sorting or similar-neighbour
ordering moved the size by at most 1.3%. Merging the former `fluids` and
`quality` sheets into `core` saved 3 KiB and two requests; the deduplication
of identical pictures dropped 20 of 916 drawn cells.

For an object-storage/CDN deployment, upload the contents of
`public/game-icons/` to a single versioned prefix, such as
`factorio/2.1.24/`. Set `PUBLIC_GAME_ICONS_BASE_URL` at web build time to that
versioned prefix; the generated component points to the image files there.
Keep old versioned prefixes while cached app pages may still refer to them,
and serve these immutable files with long-lived cache headers.
