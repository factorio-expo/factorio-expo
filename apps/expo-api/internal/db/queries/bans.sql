-- name: GetActiveUserBan :one
-- The account's ban that blocks an action, if any: the longest-running one (a
-- permanent ban first). actions is every action it bans, for the address bans
-- it hands out.
SELECT reason_code, expires_at, actions
FROM user_bans
WHERE user_id = sqlc.arg(user_id)::text
  AND ban_active(user_bans)
  AND sqlc.arg(action)::text = ANY (actions)
ORDER BY expires_at DESC NULLS FIRST
LIMIT 1;

-- name: GetActiveIPBan :one
SELECT reason_code, expires_at
FROM ip_bans
WHERE ip_hash = sqlc.arg(ip_hash)::bytea
  AND action = sqlc.arg(action)::text
  AND expires_at > now();

-- name: BanIP :exec
-- Bans an address for each action until expires_at; an existing ban of the
-- address for an action is extended, never shortened.
INSERT INTO ip_bans (ip_hash, action, reason_code, expires_at)
SELECT sqlc.arg(ip_hash)::bytea, action, sqlc.arg(reason_code)::text, sqlc.arg(expires_at)::timestamptz
FROM unnest(sqlc.arg(actions)::text[]) AS action
ON CONFLICT (ip_hash, action) DO UPDATE
    SET reason_code = excluded.reason_code,
        expires_at = greatest(ip_bans.expires_at, excluded.expires_at);

-- name: PruneExpiredIPBans :execrows
DELETE FROM ip_bans WHERE expires_at < now();

-- Ban holds (ban_holds): a deleted banned account's username and provider
-- identities as keyed hashes (internal/banhold).

-- name: ActiveBanEnd :one
-- When the account's last active ban ends, for its ban holds: null without
-- one; a permanent ban holds for one year.
SELECT (CASE WHEN bool_or(expires_at IS NULL) THEN now() + interval '1 year'
             ELSE max(expires_at) END)::timestamptz AS ends_at
FROM user_bans
WHERE user_id = $1 AND ban_active(user_bans);

-- name: HoldBan :exec
-- Holds each hash until expires_at; an existing hold is extended, never
-- shortened.
INSERT INTO ban_holds (hash, kind, expires_at)
SELECT hash, sqlc.arg(kind)::text, sqlc.arg(expires_at)::timestamptz
FROM unnest(sqlc.arg(hashes)::bytea[]) AS hash
ON CONFLICT (hash) DO UPDATE
    SET expires_at = greatest(ban_holds.expires_at, excluded.expires_at);

-- name: BanHeld :one
SELECT EXISTS (SELECT 1 FROM ban_holds WHERE hash = $1 AND expires_at > now());

-- name: PruneExpiredBanHolds :execrows
DELETE FROM ban_holds WHERE expires_at < now();
