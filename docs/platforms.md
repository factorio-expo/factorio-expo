# Space platforms

The bridge's analysis phase classifies each leaf and stores the result in
`blueprint_stats.production.space_platform`. `is_space_platform` is always
present. Platform snapshots add `weight_tons`, `thrust_mn`, `max_speed_km_s`;
non-platform snapshots omit them. Width is not collected, stored, shown or indexed.

## Materialisation

Ghosts are revived at the centred blueprint origin in tiers (ramps/supports, elevated
rails, the rest, rolling stock) with retry passes; failed revivals count as
unresolved in the analysis (`entity_not_materialized`). A platform's JSON is not re-centred. After
foundation placement the hub is created at its imported blueprint coordinates
(the game moves an imported hub to the nearest point of a 256-tile grid,
normally the origin) and quality, replacing any starter hub; the live hub is
included in analysis.

The ship is built with absolute snapping on a 256-tile grid at the origin, so
every ghost lands at its blueprint coordinates, over the foundation tiles and
next to the hub placed there. Without it the game snaps a blueprint with a hub
to a 256-tile grid around the hub and moves it by whole cells towards the
build position: a platform longer than about 512 tiles was built off its
foundation (a 3,900-tile platform landed 1,792 tiles off and
lost a fifth of its entities). The bridge logs a `ghosts not revived` line with
the count per entity name when revival leaves ghosts behind.

## Exposure

- Leaf detail returns `production.space_platform`; the leaf page shows weight, max thrust and theoretical max speed.
- Search puts an analysed platform leaf's metrics on its own `blueprints` document (`has_platform`, see [search.md](search.md)), so all platform constraints match the same leaf; any metric filter implies platform-only.
- The leaderboard's ship boards rank these metrics (fastest, thrust-to-weight, lightest) and the leaf's cargo bay count, for platforms that move (top speed above zero), from PostgreSQL ([api.md](api.md#profiles-leaderboards-challenges)).
- Values are theoretical maxima: every placed thruster at maximum performance and quality multiplier. Fuel, fluid reserves, destination and travel phase are not modelled. The bridge requires Factorio and Space Age 2.1.24+; content set `space-age`.

## Maximum speed (Factorio 2.1.24)

Drag depends on mass, not width, with no linear speed term
([release discussion](https://forums.factorio.com/viewtopic.php?p=705381#p705381)).
Per-tick thrust, weight in kg, speed in km/tick:

```text
drag_coefficient = (weight / 200)^0.8
drag = (90 * speed^2 * drag_coefficient + 10000) * sign(speed)
final_thrust = thrust / (1 + weight / 10000000)
acceleration = (final_thrust - drag) / weight / 60
max_speed_km_s = 60 * sqrt((final_thrust - 10000) / (90 * drag_coefficient))
```

`LuaSpacePlatform.weight` is already kg (`weight_tons` = /1000). If
`final_thrust <= 10000` max speed is 0; weight must be positive.

Per-tick thrust per thruster: `effectivity * fluid_usage * (fuel_energy + oxidizer_energy) * quality_multiplier`
(fluid filters' prototype fuel values, joules). Vanilla normal thruster at max:
`0.51 * 2 * (50000 + 50000) = 102000`. Quality multipliers: normal 1.0, uncommon 1.3,
rare 1.6, epic 1.9, legendary 2.5.

Public `thrust_mn` = total * 60 / 1,000,000 (6.12 MN normal, 15.3 MN legendary
thruster). Do not feed this display value back into the acceleration formula.

The preview platform is destroyed when its job completes (`job.lua` `finish_if_complete` -> `surface.lua` `drop`; also before the next job creates one).
