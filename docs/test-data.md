# Test data index

Fixtures in `test-data/` (blueprint exchange strings and related files). Fill in
the description column when you learn what a file exercises; keep each to one
line (what it contains, which tests or manual checks use it). Add a row for every
new fixture and delete rows for removed ones.

Every blueprint here is the maintainer's own work or generated for this
repository.

## Used by tests

| File | Size | Description |
|---|---|---|
| `bp-starter-smelter.txt` | 1 KB | Generated: "Starter smelter", 38 entities: five stone furnaces between a coal/ore belt and a plate belt. The default single blueprint: most Go tests and e2e `BLUEPRINT`. |
| `bp-science-clean.txt` | 1 KB | Generated: "Red science 5+1", 71 entities: 5 automation science assemblers and a gear assembler sharing one output belt. A second, different blueprint: `blueprint/tree_test.go` (assembler count), e2e `OTHER_BLUEPRINT`. |
| `bp-green-circuit-block.txt` | 1 KB | Generated: "Green circuits 3:2", 47 entities: 3 copper cable assemblers inserting directly into 2 electronic circuit assemblers; tiles east every 9 tiles. A third distinct blueprint for the result-reuse and render-queue tests (`catalog/reuse_test.go`, `httpapi/revisions_test.go`). |
| `bp-parametric-assembler-row.txt` | 1 KB | Generated: 6 assembling machine 3s whose recipes are 6 parameters of different kinds (id, ingredient-of, item-/fluid-ingredient-of, product-of, item-product-of); `blueprint/parameters_test.go`, `catalog/challenges_test.go`, `catalog/xp_test.go`, e2e `BLUEPRINT_21`. |
| `art-planet-mosaic.txt` | 7 KB | Generated: tiles only (no entities), a ringed planet in concrete, refined and hazard concrete; `catalog/reuse_test.go`. |
| `book-starter-kit.txt` | 4 KB | Generated book "Starter kit" of 7 blueprints: two leaves both labelled "Starter smelter", a "Power" sub-book, an empty "Drafts" sub-book, icons, active index 3. The Go tests' default book (parse, limits, re-encode, catalog, search). |
| `book-parametric-crafts.txt` | 2 KB | Generated book "Parametric crafts": sub-books "Assemblers" (3 cells) and "Fluids" (3 cells) plus 4 loose parametrised blueprints, 10 leaves; `httpapi/tools_test.go`, e2e `BOOK`. |
| `ship-hauler.txt` | 40 KB | The maintainer's 2.1 space platform "[planet=aquilo] The frost ship" (1,825 entities, 4,602 foundation tiles). Leaderboard tests (`catalog/leaderboards_test.go`, e2e `leaderboards.spec.ts`), `catalog/reuse_test.go`, e2e `PLATFORM` (tag suggestions in `upload.spec.ts`) and the recovery suite, which needs a job slow enough to crash Factorio during it. Also in `book-all.txt`. |
| `planner-deconstruct-set.txt` | 1 KB | Generated book of 13 deconstruction planners, the first "Cliffs" (`cliff`): trees and rocks only, items on the ground, item requests, entity and tile ghosts, a blacklist ("Everything but the grid"), quality filters, tile-only planners (one "Space platform"); planner parser, catalog and e2e (`DECONSTRUCTION_BOOK`). |
| `planner-upgrade-set.txt` | 1 KB | Generated book "Upgrade set" of 7 upgrade planners, 3 at the top (the first "Below uncommon to uncommon") and the rest in sub-books "Quality" and "Modules": quality conditions `<`, `=`, `>` and any, module item mappings; planner parser, catalog, search and e2e (`UPGRADE_BOOK`). |
| `legacy-2x-compat-ok.txt` | 1 KB | Generated 2.0.55: one inserter circuit-enabled, one reading its hand: `blueprint.Compat21` is `compatible` (`compat_test.go`). |
| `legacy-2x-compat-blocked.txt` | 1 KB | Generated 2.0.55: one inserter circuit-enabled and reading its hand (an input and an output flag on one entity): `incompatible` (`compat_test.go`). |
| `icon-tokens.json` | 3 KB | Shared cases (parse, format, scan, strip, valid icon types/qualities) for the rich-text icon grammar; read by `internal/icontoken` (Go) and `packages/expo-ui/src/game/icon-token.test.ts`. |
| `analysis/analysis-succeeded.json` | 2 KB | Golden analysis result as the Lua encoder writes it: item + fluid rows, the 320/3 vs 106.5 precision case, non-platform, a footprint and a build cost (items, raw item and fluid). Written by `tests/analysis-golden.lua`; decoded by `analysisresult`, runner `artifacts` and API tests. |
| `analysis/analysis-platform.json` | 1 KB | Golden analysis result with `state: partial` and a space-platform profile. |
| `analysis/analysis-failed.json` | 0.2 KB | Golden bridge-reported analysis failure (`analysis_failed`). |

`analysis/*.json` are regenerated with
`lua factorio/factorio-expo-bridge/tests/analysis-golden.lua factorio/factorio-expo-bridge --write test-data/analysis`
and the same Lua test fails when the checked-in files drift from the encoder.

## Manual checks and showcase

| File | Size | Description |
|---|---|---|
| `bp-balancer-4x4.txt` | 1 KB | The maintainer's classic 4-4 throughput-unlimited balancer (32 express-tier entities: six splitters, two underground crossings), labelled "4-4 balancer"; also in `book-starter-kit.txt`. |
| `bp-solar-accumulator-tile.txt` | 1 KB | Generated: tileable 18x18 field of 24 solar panels, 20 accumulators and a substation; `snap-to-grid` 18. |
| `bp-steam-hall.txt` | 1 KB | Generated: 8 boilers, 16 steam engines, burner-inserter coal feed, water pipe from the west. |
| `art-pixel-logo.txt` | 4 KB | The maintainer's "EXPO logo": the word in a 5x7 pixel font, 900 tiles (two per pixel), outlined by 58 coloured lamps. Also in `book-all.txt`. |
| `mod-kr-platform.txt` | 3 KB | Generated: small space platform (hub, cargo bays, thrusters, crushers) with Krastorio 2 Spaced Out entities (`kr-*`, dropped from the render); mod detection. No asteroid collectors and its bounding box is centred north of the hub: otherwise the bridge builds only the hub. |
| `planner-belt-upgrade.txt` | 0.5 KB | Generated: one upgrade planner, belts, undergrounds and splitters up one tier. |
| `legacy-pre2-book.txt` | 2 KB | Generated 0.17.79 book (8-way directions, typed icons) of 3 blueprints; old-format coverage. |
| `book-tile-set.txt` | 12 KB | Generated book: solar tile, green circuit block, both art blueprints. |
| `stress-megabase-slice.txt` | 60 KB | Generated stress blueprint: 8,520 entities (120 green circuit blocks, 64 solar tiles). |
| `bp-trains-vertical.txt` | 1 KB | Generated 2.0.55 blueprint: one vertical train (locomotive, 2 cargo wagons, fluid wagon), no rails; rolling-stock-only auto-rails (`train-rails.lua`), manual render check. |
| `bp-trains-east-west.txt` | 1 KB | Generated: one horizontal train with a locomotive at each end, no rails; rotated to the vertical rail. |
| `bp-trains-diagonal.txt` | 1 KB | Generated: one 45-degree train of five cars (incl. an artillery wagon), no rails; the case that would need diagonal rail. |
| `bp-trains-multiple.txt` | 1 KB | Generated: five separate trains at mixed angles (vertical, reversed, horizontal, 135 degrees, a lone wagon); one rail column each. |
| `bp-trains-uncoupled.txt` | 1 KB | Generated: five wagons in a column with gaps over 8.5 tiles, so they split into three trains. |
| `bp-trains-turned-wagons.txt` | 1 KB | Generated: one train whose wagons face both ways; facing is kept relative to the train. |
| `bp-empty.txt` | 1 KB | Generated: a blueprint with no entities or tiles; renders as an empty image instead of failing. |

## Seed books

| File | Size | Description |
|---|---|---|
| `book-all.txt` | 167 KB | "The Factory Floor Collection", the seed book for the new site: 22 flat leaves (icons `assembling-machine-3`, `space-platform-hub`), 2.1.24. Both art blueprints, the balancer, the early builds (`bp-starter-smelter`, `bp-steam-hall`, `bp-green-circuit-block`, `bp-solar-accumulator-tile`, `bp-bot-mall`), the production builds below and four ships. |
| `book-fleet.txt` | 73 KB | "Fleet": a flat book of the three ships `ship-starter`, `ship-courier` and `ship-science-lab`. |

## The maintainer's builds

Real in-game exports (2.1.24, entities / tiles), each also in `book-all.txt`;
no test reads them except `ship-hauler`:

| File | Label | Contents |
|---|---|---|
| `bp-blue-circuit-tower.txt` | Modular blue circuits | 150 / 0, electromagnetic plants making processing units with beacons. |
| `bp-bot-mall.txt` | Starter bot mall | 99 / 0, early-game bot-based mall. |
| `bp-city-block-tile.txt` | Mini City Block | 16 / 1,296, 36x36 city block base, `snap-to-grid` 36. |
| `bp-fulgora-scrapyard.txt` | `[planet=fulgora]` Scrapyard | 392 / 0, scrap processing with a sorter. |
| `bp-gleba-bioflux.txt` | `[planet=gleba]` Bioflux farm | 856 / 0, agricultural towers and biochambers. |
| `bp-legendary-circuits.txt` | Legendary circuit upcycler | 125 / 0, quality-module electromagnetic plants. |
| `bp-nuclear-quad.txt` | Nuclear power plant | 330 / 0, four reactors, 48 heat exchangers, 96 turbines. |
| `bp-oil-complex.txt` | Tileable Oil Refinery | 552 / 0, compact refinery with beacons. |
| `bp-rail-junction-elevated.txt` | Elevated T junction | 143 / 0, elevated rails with signals, `snap-to-grid` 78. |
| `bp-rocket-silo-pad.txt` | Tileable rocket launcher | 54 / 0, silo ringed by speed beacons. |
| `bp-vulcanus-foundry.txt` | `[planet=vulcanus]` Compact turbo belts | 276 / 0, turbo belt, underground and splitter build. |
| `ship-starter.txt` | `[recipe=rocket-part]` Small cargo ship | 774 / 1,611, early inner-planet cargo ship. |
| `ship-courier.txt` | Hammer-head rare ship | 712 / 1,527, rare-quality ship, modest cargo. |
| `ship-science-lab.txt` | The science space station | 2,235 / 6,688, space science ship with four asteroid collector arms. |
