-- Global settings (internal/settings defines the keys and types).

-- name: ListGlobalSettings :many
SELECT key, value, updated_at, updated_by
FROM global_settings
ORDER BY key;

-- name: UpsertGlobalSetting :exec
INSERT INTO global_settings (key, value, updated_by)
VALUES (@key, @value, @updated_by)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by;
