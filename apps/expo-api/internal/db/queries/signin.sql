-- Sign-in methods: provider identities and passkeys (internal/accounts).

-- name: GetIdentityUser :one
-- The account a provider identity belongs to. Deleting an account deletes
-- its identities, so the provider account may sign up anew at once.
SELECT u.id, u.username
FROM auth_identities i
JOIN users u ON u.id = i.user_id
WHERE i.provider = $1 AND i.provider_user_id = $2 AND u.deleted_at IS NULL;

-- name: CreateIdentity :exec
INSERT INTO auth_identities (provider, provider_user_id, user_id)
VALUES ($1, $2, $3);

-- name: TouchIdentity :exec
UPDATE auth_identities SET last_used_at = now()
WHERE provider = $1 AND provider_user_id = $2;

-- name: ListUserIdentities :many
SELECT provider, created_at, last_used_at
FROM auth_identities
WHERE user_id = $1
ORDER BY provider;

-- name: DeleteUserIdentity :execrows
DELETE FROM auth_identities WHERE user_id = $1 AND provider = $2;

-- name: CountSignInMethods :one
SELECT ((SELECT count(*) FROM auth_identities i WHERE i.user_id = $1)
        + (SELECT count(*) FROM user_passkeys p WHERE p.user_id = $1))::bigint AS methods;

-- name: CreatePasskey :exec
INSERT INTO user_passkeys (id, user_id, credential_id, credential)
VALUES ($1, $2, $3, $4);

-- name: ListUserPasskeys :many
SELECT id, credential, created_at, last_used_at
FROM user_passkeys
WHERE user_id = $1
ORDER BY created_at, id;

-- name: UpdatePasskeyAfterSignIn :exec
-- Stores the credential's new sign count and flags.
UPDATE user_passkeys SET credential = $3, last_used_at = now()
WHERE user_id = $1 AND credential_id = $2;

-- name: DeleteUserPasskey :execrows
DELETE FROM user_passkeys WHERE user_id = $1 AND id = $2;

-- name: SetRecoveryCodeHash :exec
-- Issues an account's first recovery code (at its creation).
UPDATE users SET recovery_code_hash = $2 WHERE id = $1 AND deleted_at IS NULL;

-- name: GetRecoveryAccount :one
-- The live account a recovery names: the username in any case.
SELECT id, username, recovery_code_hash
FROM users
WHERE lower(username) = lower($1) AND deleted_at IS NULL;

-- name: RotateRecoveryCodeHash :execrows
-- Uses up the code with the used hash (replacing it with the new one): zero
-- rows when another request used it first.
UPDATE users SET recovery_code_hash = sqlc.arg(new_hash)
WHERE id = sqlc.arg(id) AND recovery_code_hash = sqlc.arg(used_hash) AND deleted_at IS NULL;

-- name: ReplaceRecoveryCodeHash :execrows
-- Regenerates a code: the old one stops working.
UPDATE users SET recovery_code_hash = $2 WHERE id = $1 AND deleted_at IS NULL;
