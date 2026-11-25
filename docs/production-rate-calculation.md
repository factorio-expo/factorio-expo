# Production analysis

How the bridge computes production rates, power and pollution, and the public
result contract. Persistence: [schema.md](schema.md) (`blueprint_stats`).

## Decision

Rate calculation is an attributed extraction of
[Rate Calculator](https://codeberg.org/raiguard/RateCalculator) (MIT, Caleb Heuer)
3.4.2, commit `0054c30a05927ac809d691d9ea8d8e0bc00cc44d`, adapted into
`factorio/factorio-expo-bridge/scripts/` (`analysis*.lua`, `rate-calculator/`).
The MIT notice is in `factorio/factorio-expo-bridge/THIRD_PARTY_NOTICES.md`.
Only Lua calculation code is reused (no GUI, `flib`, icons or assets).

Why not alternatives:

- The upstream mod has no `remote` interface or command; its entry point is a player selection event with per-player GUI state. Automating its GUI is brittle.
- A Go port would need all runtime state (mod-resolved prototypes, quality, modules, beacons, force bonuses, fluidbox temperatures, surface properties); blueprint strings do not carry it.
- A fork exposing a remote interface adds `flib` and another server+client mod to keep in sync for little gain.

Formulas run in Factorio against live revived entities, so modded prototypes,
quality and runtime bonuses are authoritative. Go only validates, persists and
presents. Attribution rules: keep the MIT notice, list adapted files, record
upstream version/commit, document modifications.

## Flow

Preview and analysis share one materialisation but have independent state,
artifacts, imports and retries. The bridge revives entities on an isolated
surface (modules inserted), waits one tick so beacon effects settle, runs
`analysis.calculate({entities, force, surface, job_id, ...})` returning plain
serialisable data, and writes an atomic analysis result. A missing graphical
client does not stop analysis. Fresh analysis force-enables recipes and resets
research and bonuses to zero.

## Semantics

Results are **theoretical configured-machine maxima per second**. They do not
model logistics, belts/inserters/trains/pipes, power availability, circuits,
starvation or blocking. Each material tracks `input` and `output` totals with
machine counts; classification follows Rate Calculator: output only = product,
input only = ingredient, both = intermediate with
`difference_per_second = output - input`. Rates are unrounded floats; the bridge encodes them with the shortest representation that round-trips (`scripts/json.lua`), and Go cross-checks the difference within a relative `1e-9` (`analysisresult`). Modules and beacons count through live entity
bonuses (speed, productivity, consumption, pollution, quality).

Environment policies:

| Area | Policy |
|---|---|
| Recipes, modules, quality | As revived from the blueprint; item/entity/module/product quality preserved |
| Beacons, reactors | Live effects / actual neighbour arrangement after all entities exist |
| Power | Report theoretical maximum; availability not required |
| Fuel | Never invented (a blueprint carries none): a burner without fuel has no fuel rates, and its pollution is the full-power figure with a fuel emissions multiplier of 1 (the wiki's table). Not unresolved |
| Fluids | Recipe/fluidbox filters when unambiguous; a boiler, generator or fluid energy source whose fluid stays ambiguous (vanilla and Space Age resolve through the prototype filter, so in practice a mod) is unresolved (`fluid_unresolved`). An offshore pump's fluid depends on the tile (water, lava, ammonia) and is not in a blueprint: no rate, not unresolved (its ghost cannot revive on the water-less surface either, which is not counted as unmaterialized). A filter names only the fluid (Factorio 2.1.22+), so the temperature is the recipe's or the fluid's default. Temperatures only size the amounts (boilers, fluid energy sources); a fluid's identity is its name, so one fluid at several temperatures sums into one row and steam from boilers cancels against steam into engines and turbines as an intermediate |
| Boilers | Heat from the prototype's minimum input temperature (else the fluid's) to its target temperature; input and output amounts are each the energy over that fluid's heat capacity (2.1 water 2 kJ, steam 0.2 kJ: 6 water/s become 60 steam/s); a `heat-fluid-inside` boiler reports the same fluid in and out, so it is an intermediate (upstream 3.4.2 reports no fluid for it) |
| Crafters without a recipe | Furnaces (stone, steel, electric, recyclers) never carry a recipe in a blueprint, and other machines may have none: no rates, electricity and pollution kept (recipe multiplier 1). Not unresolved |
| Mining drills, labs | No ore on the lab surface, no active research: no rates, electricity and pollution kept. Not unresolved |
| Unavailable entity prototypes | Replaced by a display panel for the preview; each marks materials, electricity and pollution unresolved (`unavailable_prototype`) |
| Solar | Pinned analysis surface solar multiplier |
| Space platforms | Platform surface properties; see [platforms.md](platforms.md) |

`partial` means the analysis failed on something, never that the blueprint
lacks information: a blueprint cannot carry a furnace's recipe, a burner's fuel,
the ore under a drill, the research of a lab or the tile of an offshore pump, so
those are not unresolved. Only technical causes count, in the metrics they
affect (`completeness`), each recorded in `completeness.unresolved[]`
(`{cause, entity, count}`, absent when complete): `unavailable_prototype` (a mod
entity swapped for a display panel), `entity_not_materialized` (a ghost that
never became an entity), `missing_modules` (module requests that could not be
filled), `calculation_error` (the calculator threw on the entity),
`unknown_material` (a rate for an item or fluid that does not exist),
`invalid_value` (a computed rate that is not a finite positive number) and
`fluid_unresolved` (see Fluids). The analysis reports no warnings or
assumptions: the numbers are a good estimate, not a guarantee,
like other blueprint sites show. Known non-production entities are ignored. Unknown materials appear in the UI as `Unknown ×N` intermediates with
no invented rates. Known upstream
quirks (non-normal-quality beacon power, duplicate same-item outputs, fluid-energy
plus fluid-output crafters, lab rates) mean upstream parity is not proof of
correctness; keep project-owned regression cases.

## Persistence and failure

Analysis is API-owned durable data tied to the blueprint and inherited content set
(only pinned vanilla and Space Age sets). Stored: schema version (1), state
`complete|partial`, stale flag, calculation time and the rates. A failed refresh leaves an older snapshot ready but stale; an
initial failure is publicly unavailable without failure detail. Only transient
runner/RCON/client/filesystem/transport errors retry; timeouts and
calculation/validation failures are terminal until an internal recompute. No
automatic backfill after analyzer upgrades.

## Public contract

Only `GET /api/v1/entries/{entryID}/blueprints/{blueprintID}` returns production
(leaf must belong to the entry; HTTP 200 for pending and failed; ETag). Entry and
tree responses carry none. Availability is `pending|ready|failed`. Ready has
`schema_version` (1), `state` (`complete|partial`), `stale`, and the snapshot as
emitted by the bridge (`analysis.lua` is the exact schema):

```text
ingredients[]     {type,name,quality, input_per_second, consumer_count}
products[]        {type,name,quality, output_per_second, producer_count}
intermediates[]   both sets of fields, plus difference_per_second = output - input
electricity       {consumption_watts, production_watts, net_watts, drain_watts, completeness}
pollution         {pollutant, production/absorption/net_per_second, not_applicable,
                   completeness, by_pollutant[]}
completeness      {materials, electricity, pollution, unresolved[]: {cause, entity, count}}
space_platform    see platforms.md
footprint         {width, height}: tile-aligned size of everything placed
build_cost        {items[] {type,name,quality,count}, raw[] {type,name,amount}}
```

`footprint` and `build_cost` come from the materialisation, not the rate
calculator; [bridge-protocol.md](bridge-protocol.md) has their rules. Raw
resources follow each material's recipe of the same name at normal quality and
without productivity; a material without one (petroleum gas, molten iron,
ores) is listed as it is. Snapshots stored before these existed omit them.

The bridge result also names the calculator (upstream version/commit) and
`content_set`; the API keeps the inherited content set and schema version but
not calculator-build provenance. Items carry quality; fluids carry null
quality. No material carries a temperature: a fluid is its name, its rates at
every temperature summed.
Rates keep Rate Calculator's emitted order. Heat is not a v1 metric.
`electricity` gives what is made and what is used (drain included) beside
their difference; the leaf page shows all three, and each product's (and
intermediate's) output rate per tile of `footprint` and per MW of
`consumption_watts` (`rateDivisors` in `apps/expo-web/src/lib/utils/production.ts`;
neither when the footprint is missing or nothing is consumed). The search
index stores the same ratios ([search.md](search.md#collections)).
`pollutant` names the surface's tracked type; combined rates sum every type any
entity emits (e.g. agricultural towers emit `spores` on a `pollution` surface)
and `by_pollutant` breaks them out.

## Tests

Cover: assemblers with and without modules, speed/productivity/quality modules,
beacons, probabilistic/multi-product recipes, productivity caps, fluids at
several temperatures (one row, `tests/fluid-api.lua`), furnaces with restored recipes, burners with/without fuel,
boilers/generators/solar/reactors, labs, mining drills, higher-quality entities,
vanilla and Space Age, duplicate outputs, very large and unknown/modded
blueprints. Fixtures live in `test-data/`; bridge Lua tests in
`factorio/factorio-expo-bridge/tests/`.
