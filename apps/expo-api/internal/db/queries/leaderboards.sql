-- name: ListBoardCandidates :many
-- The candidates of the leaderboard (catalog/leaderboards.go): every leaf in
-- the current revision of a listed entry with a complete analysis of a space
-- platform, or of a build that makes science packs or more electricity than
-- it uses, by entry and leaf order. The condition repeats the partial index
-- idx_blueprint_stats_board_candidates word for word. The analysis and mod
-- detection are parsed and ranked in Go (internal/analysisresult is their
-- only parser).
SELECT entries.id AS entry_id,
       entries.owner_user_id,
       entries.title AS entry_title,
       revisions.created_at AS revision_created_at,
       coalesce(revisions.tree->>'kind', '')::text AS root_kind,
       blueprints.id AS blueprint_id,
       blueprints.label,
       blueprints.description,
       blueprints.icons,
       blueprint_stats.entities,
       blueprint_stats.mods,
       blueprint_stats.mods_version,
       blueprint_stats.production,
       blueprint_stats.production_schema_version::integer AS production_schema_version
FROM entries
JOIN revisions ON revisions.id = entries.current_revision_id
JOIN blueprints ON blueprints.revision_id = revisions.id
JOIN blueprint_stats ON blueprint_stats.blueprint_id = blueprints.id
WHERE entry_listed(entries)
  AND blueprint_stats.production_state = 'complete'
  AND (blueprint_stats.production @> '{"space_platform": {"is_space_platform": true}}'
       OR (blueprint_stats.production->'electricity'->>'net_watts')::double precision > 0
       OR jsonb_path_exists(blueprint_stats.production, '$.products[*] ? (@.name like_regex "-science-pack$")')
       OR jsonb_path_exists(blueprint_stats.production, '$.intermediates[*] ? (@.name like_regex "-science-pack$")'))
  AND blueprint_stats.production_schema_version IS NOT NULL
ORDER BY entries.id, blueprints.position;

-- name: ListBoardLeaders :many
-- The stored leader of every board that has one (at most one row per board:
-- the leaderboard and every card's record chips read the whole table).
SELECT * FROM board_leaders ORDER BY board;

-- name: UpsertBoardLeader :one
-- Records a board's leader; since moves only when another entry takes the lead.
INSERT INTO board_leaders (board, entry_id, blueprint_id, user_id, value)
VALUES (sqlc.arg(board), sqlc.arg(entry_id), sqlc.arg(blueprint_id), sqlc.narg(user_id), sqlc.arg(value))
ON CONFLICT (board) DO UPDATE SET
    entry_id = excluded.entry_id,
    blueprint_id = excluded.blueprint_id,
    user_id = excluded.user_id,
    value = excluded.value,
    since = CASE WHEN board_leaders.entry_id = excluded.entry_id THEN board_leaders.since ELSE now() END
RETURNING *;

-- name: DeleteBoardLeadersExcept :exec
-- Drops the leader rows of boards that have none now (fewer than 10 entries).
DELETE FROM board_leaders WHERE NOT (board = ANY(sqlc.arg(boards)::text[]));

-- name: ListUserRecords :many
-- The boards a user leads now, with a listed entry.
SELECT board_leaders.board,
       board_leaders.entry_id,
       board_leaders.blueprint_id,
       board_leaders.value,
       board_leaders.since,
       entries.title AS entry_title
FROM board_leaders
JOIN entries ON entries.id = board_leaders.entry_id
WHERE board_leaders.user_id = $1
  AND entry_listed(entries)
ORDER BY board_leaders.board;
