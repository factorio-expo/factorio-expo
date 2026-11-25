-- name: NextCommentNumber :one
-- Hands out the entry's next comment number. The caller already holds the
-- entry row lock (GetEntryForUpdate), which also orders comments against a
-- visibility change that purges them.
UPDATE entries
SET comment_seq = comment_seq + 1
WHERE id = $1
RETURNING comment_seq;

-- name: CreateComment :one
INSERT INTO comments (id, entry_id, number, author_user_id, revision_number, body)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetCommentForUpdate :one
SELECT *
FROM comments
WHERE entry_id = $1 AND number = $2
FOR UPDATE;

-- name: UpdateCommentBody :one
UPDATE comments
SET body = $2, edited_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: RemoveComment :execrows
-- Leaves a tombstone: the number stays, the body is wiped.
UPDATE comments
SET body = '', deleted_at = now(), deleted_by = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: DeleteEntryComments :execrows
-- An entry that stops being public loses its thread (reports cascade).
DELETE FROM comments WHERE entry_id = $1;

-- name: ListCommentsAfter :many
-- One page of a thread in number order, from just after a number.
SELECT comments.*, users.username AS author_username
FROM comments
JOIN users ON users.id = comments.author_user_id
WHERE comments.entry_id = $1 AND comments.number > sqlc.arg(after)::integer
ORDER BY comments.number
LIMIT $2;

-- name: ListCommentsBefore :many
-- One page of a thread ending just before a number, newest first (the caller
-- reverses it).
SELECT comments.*, users.username AS author_username
FROM comments
JOIN users ON users.id = comments.author_user_id
WHERE comments.entry_id = $1 AND comments.number < sqlc.arg(before)::integer
ORDER BY comments.number DESC
LIMIT $2;

-- name: GetCommentBounds :one
-- The first and last comment numbers and how many there are; 0 for none.
SELECT coalesce(min(number), 0)::integer AS first_number,
       coalesce(max(number), 0)::integer AS last_number,
       count(*)::bigint AS total
FROM comments
WHERE entry_id = $1;

-- name: GetUserCommentActivity :one
-- What the daily and duplicate caps need: comments the user posted in the last
-- day (in total and on this entry, removed ones included so deleting does not
-- reset the budget), and whether one in the last ten minutes had this body.
SELECT count(*)::bigint AS day_total,
       count(*) FILTER (WHERE entry_id = sqlc.arg(entry_id))::bigint AS day_on_entry,
       coalesce(bool_or(body = sqlc.arg(body)::text
           AND created_at > now() - interval '10 minutes'), false)::boolean AS duplicate
FROM comments
WHERE author_user_id = sqlc.arg(author_user_id)
  AND created_at > now() - interval '1 day';

-- name: CreateCommentReport :execrows
-- One report per user per comment; reporting again changes nothing.
INSERT INTO comment_reports (comment_id, reporter_user_id, reason, note, body_snapshot)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (comment_id, reporter_user_id) DO NOTHING;
