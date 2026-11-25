-- name: CreateUser :one
INSERT INTO users (id, username)
VALUES ($1, $2)
RETURNING *;

-- Every lookup below skips deleted accounts: they cannot sign in and their
-- profile is gone (their sessions stop working too: sessions.sql). Staff read them through
-- staffusers.sql.

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1 AND deleted_at IS NULL;

-- name: GetUserByUsername :one
-- Usernames are unique case-insensitively, so lookups are too.
SELECT * FROM users WHERE lower(username) = lower($1) AND deleted_at IS NULL;

-- name: UsernameUnavailable :one
-- Whether an account has the username in any case (deleted accounts keep
-- theirs until they are hard-deleted), it is reserved (reserved names match
-- ignoring case, '_' and '-': reserved_usernames), or a deleted banned
-- account's ban holds it (hold_hash: internal/banhold, normalized the same
-- way).
SELECT EXISTS (
    SELECT 1 FROM users WHERE lower(username) = lower(sqlc.arg(username)::text)
    UNION ALL
    SELECT 1 FROM reserved_usernames WHERE name = translate(lower(sqlc.arg(username)::text), '_-', '')
    UNION ALL
    SELECT 1 FROM ban_holds WHERE hash = sqlc.arg(hold_hash)::bytea AND expires_at > now()
);

-- name: LockUser :one
-- Serialises changes to one account's sign-in methods (never remove the last).
SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE;
