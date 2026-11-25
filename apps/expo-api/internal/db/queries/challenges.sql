-- Design challenges (catalog/challenges.go, catalog/staffchallenges.go).

-- name: ListPublishedChallenges :many
-- Every published challenge, the newest first, with its active submissions
-- whose entry is listed.
SELECT challenges.*,
       (SELECT count(*)
        FROM challenge_submissions
        JOIN entries ON entries.id = challenge_submissions.entry_id
        WHERE challenge_submissions.challenge_id = challenges.id
          AND challenge_submissions.withdrawn_at IS NULL
          AND entry_listed(entries))::bigint AS entry_count
FROM challenges
WHERE published_at IS NOT NULL
  AND (NOT hidden_until_start OR starts_at <= now())
ORDER BY starts_at DESC, slug;

-- name: ListStaffChallenges :many
SELECT challenges.*,
       (SELECT count(*)
        FROM challenge_submissions
        WHERE challenge_submissions.challenge_id = challenges.id
          AND challenge_submissions.withdrawn_at IS NULL)::bigint AS entry_count
FROM challenges
ORDER BY starts_at DESC, slug;

-- name: GetChallengeBySlug :one
SELECT * FROM challenges WHERE slug = $1;

-- name: GetChallenge :one
SELECT * FROM challenges WHERE id = $1;

-- name: LockChallenge :one
SELECT * FROM challenges WHERE id = $1 FOR UPDATE;

-- name: InsertChallenge :one
INSERT INTO challenges (
    id, slug, title, summary, description, icons, rules,
    starts_at, ends_at, voting_ends_at, require_new_revisions, voters_before_start,
    voting_enabled, votes_per_user, live_leaderboard, hidden_until_start
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
RETURNING *;

-- name: UpdateChallenge :one
UPDATE challenges
SET slug = $2, title = $3, summary = $4, description = $5, icons = $6,
    rules = $7, starts_at = $8, ends_at = $9, voting_ends_at = $10,
    require_new_revisions = $11, voters_before_start = $12, voting_enabled = $13,
    votes_per_user = $14, live_leaderboard = $15, hidden_until_start = $16, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetChallengePublished :one
UPDATE challenges
SET published_at = CASE WHEN sqlc.arg(published)::boolean THEN coalesce(published_at, now()) END,
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteChallenge :execrows
DELETE FROM challenges WHERE id = $1;

-- name: ListChallengeSubmissionFacts :many
-- Each active submission of the challenge with the stored facts of its scored
-- revision: while the challenge is open the entry's current revision, after
-- that its newest revision made before ends_at. The blueprint columns are
-- empty when that revision is not a single blueprint. listed says whether the
-- entry is listed now (a hidden entry never shows).
SELECT challenge_submissions.id,
       challenge_submissions.entry_id,
       challenge_submissions.user_id,
       challenge_submissions.submitted_at,
       challenge_submissions.staff_verdict,
       challenge_submissions.staff_reason,
       users.username,
       entry_listed(entries)::boolean AS listed,
       coalesce(scored.id, '')::text AS revision_id,
       coalesce(scored.created_at, challenge_submissions.submitted_at)::timestamptz AS revision_created_at,
       coalesce(scored.tree->>'kind', '')::text AS root_kind,
       coalesce(blueprints.id, '')::text AS blueprint_id,
       coalesce(blueprint_stats.entities, '[]')::jsonb AS entities,
       coalesce(blueprint_stats.recipes, '[]')::jsonb AS recipes,
       coalesce(blueprint_stats.mods, '{}')::jsonb AS mods,
       coalesce(blueprint_stats.mods_version, '')::text AS mods_version,
       coalesce(blueprint_stats.production, '{}')::jsonb AS production,
       blueprint_stats.production_schema_version,
       blueprint_stats.production_state,
       coalesce(leaf_progress.has_production, false)::boolean AS has_production,
       coalesce(leaf_progress.analysis_job_state, '')::text AS analysis_job_state
FROM challenge_submissions
JOIN challenges ON challenges.id = challenge_submissions.challenge_id
JOIN entries ON entries.id = challenge_submissions.entry_id
JOIN users ON users.id = challenge_submissions.user_id
LEFT JOIN LATERAL (
    SELECT revisions.*
    FROM revisions
    WHERE revisions.entry_id = entries.id
      AND CASE WHEN challenges.ends_at > now()
               THEN revisions.id = entries.current_revision_id
               ELSE revisions.created_at <= challenges.ends_at END
    ORDER BY revisions.version_number DESC
    LIMIT 1
) AS scored ON true
LEFT JOIN blueprints ON scored.tree->>'kind' = 'blueprint' AND blueprints.id = scored.tree->>'id'
LEFT JOIN blueprint_stats ON blueprint_stats.blueprint_id = blueprints.id
LEFT JOIN leaf_progress ON leaf_progress.blueprint_id = blueprints.id
WHERE challenge_submissions.challenge_id = $1
  AND challenge_submissions.withdrawn_at IS NULL
ORDER BY challenge_submissions.submitted_at, challenge_submissions.id;

-- name: ListChallengeVoteCounts :many
-- Votes per entry of the challenge (voided votes do not count).
SELECT challenge_votes.entry_id, count(*)::bigint AS votes
FROM challenge_votes
WHERE challenge_votes.challenge_id = $1 AND challenge_votes.voided_at IS NULL
GROUP BY challenge_votes.entry_id;

-- name: ListUserChallengeVotes :many
SELECT entry_id FROM challenge_votes
WHERE challenge_id = $1 AND user_id = $2
ORDER BY created_at;

-- name: ActiveChallengeSubmission :one
SELECT * FROM challenge_submissions
WHERE challenge_id = $1 AND user_id = $2 AND withdrawn_at IS NULL;

-- name: ActiveChallengeSubmissionOfEntry :one
SELECT * FROM challenge_submissions
WHERE challenge_id = $1 AND entry_id = $2 AND withdrawn_at IS NULL;

-- name: GetChallengeSubmission :one
SELECT * FROM challenge_submissions WHERE id = $1 AND challenge_id = $2;

-- name: WithdrawChallengeSubmission :exec
UPDATE challenge_submissions
SET withdrawn_at = now(), withdrawn_reason = sqlc.arg(reason)
WHERE id = sqlc.arg(id) AND withdrawn_at IS NULL;

-- name: InsertChallengeSubmission :one
INSERT INTO challenge_submissions (id, challenge_id, entry_id, user_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: SetChallengeSubmissionVerdict :one
UPDATE challenge_submissions
SET staff_verdict = sqlc.narg(verdict), staff_reason = sqlc.arg(reason)
WHERE id = sqlc.arg(id) AND challenge_id = sqlc.arg(challenge_id)
RETURNING *;

-- name: ListEntryChallenges :many
-- The challenges an entry is actively submitted to (its page's badge).
SELECT challenges.id, challenges.slug, challenges.title
FROM challenge_submissions
JOIN challenges ON challenges.id = challenge_submissions.challenge_id
WHERE challenge_submissions.entry_id = $1
  AND challenge_submissions.withdrawn_at IS NULL
  AND challenges.published_at IS NOT NULL
ORDER BY challenges.starts_at;

-- name: LockVoter :one
-- The voter's row lock serializes their votes, so the per-challenge limit
-- holds; also whether they may vote at all.
SELECT users.id, users.created_at,
       EXISTS (
           SELECT 1 FROM user_bans
           WHERE user_bans.user_id = users.id AND ban_active(user_bans)
       )::boolean AS banned
FROM users
WHERE users.id = $1 AND users.deleted_at IS NULL
FOR UPDATE OF users;

-- name: CountUserChallengeVotes :one
-- Votes on active submissions only: a vote on a withdrawn entry frees its slot.
SELECT count(*)::bigint
FROM challenge_votes
JOIN challenge_submissions ON challenge_submissions.challenge_id = challenge_votes.challenge_id
                          AND challenge_submissions.entry_id = challenge_votes.entry_id
                          AND challenge_submissions.withdrawn_at IS NULL
WHERE challenge_votes.challenge_id = $1 AND challenge_votes.user_id = $2;

-- name: InsertChallengeVote :exec
INSERT INTO challenge_votes (challenge_id, user_id, entry_id)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING;

-- name: DeleteChallengeVote :exec
-- A voter takes back their own vote; a voided one stays (it keeps its slot).
DELETE FROM challenge_votes
WHERE challenge_id = $1 AND user_id = $2 AND entry_id = $3 AND voided_at IS NULL;

-- name: ListChallengeVotesForOversight :many
-- Every vote of the challenge on an active submission, oldest first, with the
-- voter's account age, for the console's voting oversight.
SELECT challenge_votes.user_id,
       challenge_votes.entry_id,
       challenge_votes.created_at,
       challenge_votes.voided_at,
       challenge_votes.void_reason,
       users.username,
       users.created_at AS user_created_at,
       (users.deleted_at IS NOT NULL)::boolean AS user_deleted
FROM challenge_votes
JOIN users ON users.id = challenge_votes.user_id
JOIN challenge_submissions ON challenge_submissions.challenge_id = challenge_votes.challenge_id
                          AND challenge_submissions.entry_id = challenge_votes.entry_id
                          AND challenge_submissions.withdrawn_at IS NULL
WHERE challenge_votes.challenge_id = $1
ORDER BY challenge_votes.created_at, challenge_votes.user_id, challenge_votes.entry_id
LIMIT 50000;

-- name: VoidChallengeVotes :execrows
-- Voids a voter's votes in the challenge: one entry's (entry_id) or all of
-- them. Already voided votes keep their first reason.
UPDATE challenge_votes
SET voided_at = now(), void_reason = sqlc.arg(reason)
WHERE challenge_id = sqlc.arg(challenge_id) AND user_id = sqlc.arg(user_id)
  AND (sqlc.narg(entry_id)::text IS NULL OR entry_id = sqlc.narg(entry_id)::text)
  AND voided_at IS NULL;

-- name: UnvoidChallengeVotes :execrows
-- Counts a voter's voided votes again (one entry's or all of them).
UPDATE challenge_votes
SET voided_at = NULL, void_reason = ''
WHERE challenge_id = sqlc.arg(challenge_id) AND user_id = sqlc.arg(user_id)
  AND (sqlc.narg(entry_id)::text IS NULL OR entry_id = sqlc.narg(entry_id)::text)
  AND voided_at IS NOT NULL;

-- name: GetChallengeTestFacts :one
-- The stored facts of an entry's current revision (any entry, quick previews
-- too), the console's "test a blueprint" box: the same columns as
-- ListChallengeSubmissionFacts, scored as a challenge that is still open.
SELECT coalesce(revisions.created_at, entries.created_at)::timestamptz AS revision_created_at,
       coalesce(revisions.tree->>'kind', '')::text AS root_kind,
       coalesce(blueprints.id, '')::text AS blueprint_id,
       coalesce(blueprint_stats.entities, '[]')::jsonb AS entities,
       coalesce(blueprint_stats.recipes, '[]')::jsonb AS recipes,
       coalesce(blueprint_stats.mods, '{}')::jsonb AS mods,
       coalesce(blueprint_stats.mods_version, '')::text AS mods_version,
       coalesce(blueprint_stats.production, '{}')::jsonb AS production,
       blueprint_stats.production_schema_version,
       blueprint_stats.production_state,
       coalesce(leaf_progress.has_production, false)::boolean AS has_production,
       coalesce(leaf_progress.analysis_job_state, '')::text AS analysis_job_state
FROM entries
LEFT JOIN revisions ON revisions.id = entries.current_revision_id
LEFT JOIN blueprints ON revisions.tree->>'kind' = 'blueprint' AND blueprints.id = revisions.tree->>'id'
LEFT JOIN blueprint_stats ON blueprint_stats.blueprint_id = blueprints.id
LEFT JOIN leaf_progress ON leaf_progress.blueprint_id = blueprints.id
WHERE entries.id = $1;
