-- Signed-in devices (internal/sessions): one row per session JWT.

-- name: CreateSession :exec
INSERT INTO user_sessions (id, user_id, expires_at)
VALUES ($1, $2, $3);

-- name: GetLiveSession :one
-- A session that has not expired, of an account that is not deleted.
SELECT s.id, s.user_id, s.created_at, s.last_used_at, s.expires_at, s.auth_time
FROM user_sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = $1 AND s.expires_at > now() AND u.deleted_at IS NULL;

-- name: TouchSession :exec
UPDATE user_sessions SET last_used_at = now() WHERE id = $1;

-- name: ConfirmSessionSignIn :execrows
-- The person completed a sign-in ceremony again on this session.
UPDATE user_sessions SET auth_time = now(), last_used_at = now()
WHERE id = $1 AND user_id = $2 AND expires_at > now();

-- name: ListUserSessions :many
SELECT id, created_at, last_used_at, expires_at
FROM user_sessions
WHERE user_id = $1 AND expires_at > now()
ORDER BY last_used_at DESC, id;

-- name: DeleteUserSession :execrows
DELETE FROM user_sessions WHERE id = $1 AND user_id = $2;

-- name: DeleteUserSessions :execrows
-- Signs the account out everywhere.
DELETE FROM user_sessions WHERE user_id = $1;

-- name: DeleteOtherUserSessions :execrows
-- Signs the account out everywhere but the session that asked.
DELETE FROM user_sessions WHERE user_id = $1 AND id <> sqlc.arg(keep_id)::text;

-- name: DeleteExpiredSessions :execrows
DELETE FROM user_sessions WHERE expires_at <= now();
