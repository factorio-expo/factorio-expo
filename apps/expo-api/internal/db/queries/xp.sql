-- name: LockUserXPReason :exec
-- Serializes the capped awards of one user and reason (uploads, comments) for
-- the rest of the transaction, so two at once cannot both pass the daily cap.
SELECT pg_advisory_xact_lock(hashtextextended('xp:' || sqlc.arg(user_id)::text || ':' || sqlc.arg(reason)::text, 0));

-- name: AwardXP :execrows
-- Appends one award to the ledger. Nothing is written when the source event
-- already earned (source_key), the account is deleted or under an active ban,
-- or the reason's daily cap (UTC day, every row of it counted; 0 for none)
-- is reached.
INSERT INTO xp_events (id, user_id, reason, points, source_key, entry_id, comment_id, challenge_id, board)
SELECT sqlc.arg(id)::text, users.id, sqlc.arg(reason)::text, sqlc.arg(points)::integer, sqlc.arg(source_key)::text,
       sqlc.narg(entry_id)::text, sqlc.narg(comment_id)::text, sqlc.narg(challenge_id)::text, sqlc.narg(board)::text
FROM users
WHERE users.id = sqlc.arg(user_id)::text
  AND users.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM user_bans
      WHERE user_bans.user_id = users.id
        AND ban_active(user_bans)
  )
  AND (sqlc.arg(daily_cap)::integer = 0 OR (
      SELECT count(*) FROM xp_events
      WHERE xp_events.user_id = users.id
        AND xp_events.reason = sqlc.arg(reason)::text
        AND xp_events.created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
  ) < sqlc.arg(daily_cap)::integer)
ON CONFLICT (source_key) DO NOTHING;

-- name: SummarizeUserXP :many
-- A user's ledger per reason: the rows that count now (their entry live,
-- their comment not removed) with their points, and every row ever earned.
SELECT counted.reason,
       (count(*) FILTER (WHERE counted.counts))::bigint AS counted_events,
       (coalesce(sum(counted.points) FILTER (WHERE counted.counts), 0))::bigint AS counted_points,
       count(*)::bigint AS all_events
FROM (
    SELECT xp_events.reason,
           xp_events.points,
           (xp_events.entry_id IS NULL OR (entries.id IS NOT NULL AND entry_live(entries)))
           AND (xp_events.comment_id IS NULL OR (comments.id IS NOT NULL AND comments.deleted_at IS NULL)) AS counts
    FROM xp_events
    LEFT JOIN entries ON entries.id = xp_events.entry_id
    LEFT JOIN comments ON comments.id = xp_events.comment_id
    WHERE xp_events.user_id = $1
) AS counted
GROUP BY counted.reason
ORDER BY counted.reason;

-- name: GetUserBadgeFacts :one
-- The badge facts the ledger does not hold: a listed book of at least
-- book_min_blueprints blueprints, and taking part in the first challenge
-- (earliest start).
SELECT EXISTS (
           SELECT 1
           FROM entries
           JOIN revisions ON revisions.id = entries.current_revision_id
           WHERE entries.owner_user_id = sqlc.arg(user_id)::text
             AND entry_listed(entries)
             AND revisions.tree->>'kind' = 'book'
             AND (SELECT count(*) FROM blueprints WHERE blueprints.revision_id = revisions.id) >= sqlc.arg(book_min_blueprints)::bigint
       )::boolean AS book_author,
       EXISTS (
           SELECT 1
           FROM xp_events
           WHERE xp_events.user_id = sqlc.arg(user_id)::text
             AND xp_events.reason = 'challenge_participation'
             AND xp_events.challenge_id = (
                 SELECT challenges.id FROM challenges
                 WHERE challenges.published_at IS NOT NULL
                 ORDER BY challenges.starts_at, challenges.id
                 LIMIT 1
             )
       )::boolean AS first_challenge;

-- name: ListUnawardedClosedChallenges :many
-- Published challenges whose voting has ended and whose awards are not out yet.
SELECT * FROM challenges
WHERE published_at IS NOT NULL AND awarded_at IS NULL AND voting_ends_at <= now()
ORDER BY voting_ends_at, id;

-- name: MarkChallengeAwarded :execrows
UPDATE challenges SET awarded_at = now() WHERE id = $1 AND awarded_at IS NULL;
