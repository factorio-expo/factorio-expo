-- name: GetBlueprintStat :one
SELECT * FROM blueprint_stats WHERE blueprint_id = $1;

-- name: ListBlueprintStatsByRevision :many
-- Batch-fetches every leaf's stats for one revision, keyed by blueprint_id,
-- so the search projection builder can join structural/production facts onto
-- each leaf without a query per blueprint. A blueprint with no analysis yet
-- simply has no matching row here.
SELECT blueprint_stats.*
FROM blueprint_stats
         JOIN blueprints ON blueprints.id = blueprint_stats.blueprint_id
WHERE blueprints.revision_id = $1;

-- name: UpsertBlueprintStructure :exec
-- Structural facts computed at ingestion time from the decoded blueprint
-- (entity/recipe breakdown and the mods they need). Independent of production
-- analysis: this never touches the production_* columns, and UpsertProduction
-- never touches these.
INSERT INTO blueprint_stats (blueprint_id, entities, recipes, mods, mods_version)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (blueprint_id) DO UPDATE SET
    entities = EXCLUDED.entities,
    recipes = EXCLUDED.recipes,
    mods = EXCLUDED.mods,
    mods_version = EXCLUDED.mods_version;

-- name: ListStaleModDetections :many
-- Leaves whose mod detection was made against another name table, with
-- their entry (its search document carries the detection).
SELECT bs.blueprint_id, bs.entities, bs.recipes, r.entry_id
FROM blueprint_stats bs
         JOIN blueprints b ON b.id = bs.blueprint_id
         JOIN revisions r ON r.id = b.revision_id
WHERE bs.mods_version <> sqlc.arg(mods_version)::text
ORDER BY bs.blueprint_id
LIMIT sqlc.arg(row_limit);

-- name: UpdateModDetection :exec
UPDATE blueprint_stats
SET mods = $2, mods_version = $3
WHERE blueprint_id = $1;

-- name: UpsertProduction :exec
INSERT INTO blueprint_stats (
    blueprint_id, production, production_schema_version, production_state,
    production_stale, production_calculated_at
)
VALUES ($1, $2, $3, $4, false, now())
ON CONFLICT (blueprint_id) DO UPDATE SET
    production = EXCLUDED.production,
    production_schema_version = EXCLUDED.production_schema_version,
    production_state = EXCLUDED.production_state,
    production_stale = false,
    production_calculated_at = EXCLUDED.production_calculated_at;

-- name: MarkProductionStale :exec
UPDATE blueprint_stats
SET production_stale = true
WHERE blueprint_id = $1 AND production_schema_version IS NOT NULL;
