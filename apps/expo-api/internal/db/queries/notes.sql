-- name: ListBlueprintNotes :many
-- A leaf's notes in pin order; number is the rank shown on the pin.
SELECT id, x, y, body, created_at, edited_at,
       (row_number() OVER (ORDER BY created_at, id))::integer AS number
FROM blueprint_notes
WHERE blueprint_id = $1
ORDER BY created_at, id;

-- name: CountBlueprintNotes :one
SELECT count(*)::integer FROM blueprint_notes WHERE blueprint_id = $1;

-- name: CreateBlueprintNote :one
INSERT INTO blueprint_notes (id, blueprint_id, x, y, body)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: UpdateBlueprintNote :execrows
-- Changes the text (marking the note edited), the pin, or both; a null
-- argument leaves its column alone.
UPDATE blueprint_notes
SET body = COALESCE(sqlc.narg(body)::text, body),
    x = COALESCE(sqlc.narg(x)::double precision, x),
    y = COALESCE(sqlc.narg(y)::double precision, y),
    edited_at = CASE WHEN sqlc.narg(body)::text IS NULL THEN edited_at ELSE now() END
WHERE id = sqlc.arg(id) AND blueprint_id = sqlc.arg(blueprint_id);

-- name: DeleteBlueprintNote :execrows
DELETE FROM blueprint_notes WHERE id = $1 AND blueprint_id = $2;
