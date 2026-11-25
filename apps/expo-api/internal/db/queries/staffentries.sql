-- Staff console -> Entries (admin listener only): every entry, deleted,
-- expired, private and quick previews included, one entry's page and its
-- staff actions. Every other entries query filters on the lifecycle rules.

-- name: ListStaffEntries :many
-- One page of entries, newest first, ending before a (created_at, id) cursor
-- (null starts at the newest). query matches the id, part of the title or
-- the owner's username; state is all, live, deleted or quick_preview;
-- visibility and author ('anonymous', 'account') narrow it when set;
-- failed keeps entries whose current revision has a leaf failed for good;
-- mods is '', 'vanilla' (no leaf needs a mod), 'space_age' (built-in mods
-- only: Space Age, Quality, ...) or 'modded' (a leaf needs another mod or
-- has names no known mod defines).
SELECT
    e.id, e.title, e.visibility, e.quick_preview, e.spoiler, e.created_at, e.deleted_at, e.expires_at,
    (NOT entry_live(e))::boolean AS gone,
    coalesce(o.username, '')::text AS owner_username,
    (o.deleted_at IS NOT NULL)::boolean AS owner_deleted,
    (SELECT count(*) FROM blueprints b WHERE b.revision_id = e.current_revision_id)::bigint AS leaves,
    (SELECT count(*) FROM leaf_progress lp
     WHERE lp.revision_id = e.current_revision_id
       AND (lp.preview_job_state = 'failed' OR lp.analysis_job_state = 'failed'))::bigint AS failed_leaves,
    (SELECT count(*) FROM entry_reports r WHERE r.entry_id = e.id AND r.resolved_at IS NULL)::bigint
        AS open_reports
FROM entries e
LEFT JOIN users o ON o.id = e.owner_user_id
WHERE (sqlc.arg(query)::text = ''
       OR e.id = sqlc.arg(query)::text
       OR strpos(lower(e.title), lower(sqlc.arg(query)::text)) > 0
       OR lower(o.username) = lower(sqlc.arg(query)::text))
  AND CASE sqlc.arg(state)::text
          WHEN 'live' THEN entry_lasting(e)
          WHEN 'deleted' THEN e.deleted_at IS NOT NULL
          WHEN 'quick_preview' THEN e.quick_preview
          ELSE true
      END
  AND (sqlc.arg(visibility)::text = '' OR e.visibility = sqlc.arg(visibility)::text)
  AND CASE sqlc.arg(author)::text
          WHEN 'anonymous' THEN e.owner_user_id IS NULL
          WHEN 'account' THEN e.owner_user_id IS NOT NULL
          ELSE true
      END
  AND (NOT sqlc.arg(failed)::boolean OR EXISTS (
          SELECT 1 FROM leaf_progress lp
          WHERE lp.revision_id = e.current_revision_id
            AND (lp.preview_job_state = 'failed' OR lp.analysis_job_state = 'failed')))
  AND CASE sqlc.arg(mods)::text
          WHEN 'vanilla' THEN NOT EXISTS (
              SELECT 1 FROM blueprints b JOIN blueprint_stats bs ON bs.blueprint_id = b.id
              WHERE b.revision_id = e.current_revision_id
                AND (coalesce(jsonb_array_length(bs.mods->'modsets'), 0) > 0
                     OR coalesce(jsonb_array_length(bs.mods->'unknown_entities'), 0) > 0
                     OR coalesce(jsonb_array_length(bs.mods->'unknown_recipes'), 0) > 0))
          WHEN 'space_age' THEN EXISTS (
              SELECT 1 FROM blueprints b JOIN blueprint_stats bs ON bs.blueprint_id = b.id
              WHERE b.revision_id = e.current_revision_id
                AND coalesce(jsonb_array_length(bs.mods->'modsets'), 0) > 0)
              AND NOT EXISTS (
              SELECT 1 FROM blueprints b JOIN blueprint_stats bs ON bs.blueprint_id = b.id
              WHERE b.revision_id = e.current_revision_id
                AND (coalesce(jsonb_array_length(bs.mods->'unknown_entities'), 0) > 0
                     OR coalesce(jsonb_array_length(bs.mods->'unknown_recipes'), 0) > 0
                     OR EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(bs.mods->'modsets', '[]')) m
                                WHERE NOT coalesce((m->>'builtin')::boolean, false))))
          WHEN 'modded' THEN EXISTS (
              SELECT 1 FROM blueprints b JOIN blueprint_stats bs ON bs.blueprint_id = b.id
              WHERE b.revision_id = e.current_revision_id
                AND (coalesce(jsonb_array_length(bs.mods->'unknown_entities'), 0) > 0
                     OR coalesce(jsonb_array_length(bs.mods->'unknown_recipes'), 0) > 0
                     OR EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(bs.mods->'modsets', '[]')) m
                                WHERE NOT coalesce((m->>'builtin')::boolean, false))))
          ELSE true
      END
  AND (sqlc.narg(created_from)::timestamptz IS NULL OR e.created_at >= sqlc.narg(created_from)::timestamptz)
  AND (sqlc.narg(created_to)::timestamptz IS NULL OR e.created_at < sqlc.narg(created_to)::timestamptz)
  AND (sqlc.arg(min_leaves)::integer = 0 OR (
          SELECT count(*) FROM blueprints b WHERE b.revision_id = e.current_revision_id
      ) >= sqlc.arg(min_leaves)::integer)
  AND (sqlc.narg(before_created_at)::timestamptz IS NULL
       OR (e.created_at, e.id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.arg(before_id)::text))
ORDER BY e.created_at DESC, e.id DESC
LIMIT sqlc.arg(page_size)::integer;

-- name: StaffEntryCounts :one
-- The Entries page's state tab counts.
SELECT
    count(*)::bigint AS total,
    count(*) FILTER (WHERE entry_lasting(entries))::bigint AS live,
    count(*) FILTER (WHERE deleted_at IS NOT NULL)::bigint AS deleted,
    count(*) FILTER (WHERE quick_preview)::bigint AS quick_previews
FROM entries;

-- name: GetStaffEntry :one
-- One entry as staff see it, whatever its state.
SELECT
    e.*,
    (NOT entry_live(e))::boolean AS gone,
    (entry_listed(e))::boolean AS listed,
    coalesce(o.username, '')::text AS owner_username,
    (o.deleted_at IS NOT NULL)::boolean AS owner_deleted,
    coalesce(current.version_number, 0)::integer AS current_revision_number,
    coalesce(v.views, 0)::bigint AS views,
    (SELECT count(*) FROM favorites f WHERE f.entry_id = e.id)::bigint AS favorites,
    coalesce((SELECT array_agg(t.slug ORDER BY t.slug) FROM entry_tags et JOIN tags t ON t.id = et.tag_id
              WHERE et.entry_id = e.id), '{}')::text[] AS tags,
    coalesce(sj.action, '')::text AS search_action,
    coalesce(sj.state, '')::text AS search_state,
    coalesce(sj.failure_code, '')::text AS search_failure_code
FROM entries e
LEFT JOIN users o ON o.id = e.owner_user_id
LEFT JOIN revisions current ON current.id = e.current_revision_id
LEFT JOIN entry_view_counts v ON v.entry_id = e.id
LEFT JOIN entry_search_jobs sj ON sj.entry_id = e.id
WHERE e.id = $1;

-- name: ListStaffEntryRevisions :many
-- An entry's revisions, newest first, with their sizes.
SELECT
    r.id, r.version_number, r.game_version, r.created_at,
    octet_length(r.original_string)::bigint AS string_bytes,
    (SELECT count(*) FROM blueprints b WHERE b.revision_id = r.id)::bigint AS leaves
FROM revisions r
WHERE r.entry_id = $1
ORDER BY r.version_number DESC;

-- name: ListStaffRevisionLeaves :many
-- A revision's blueprints in tree order with their job states and failures.
SELECT
    b.id, b.label, b.position, b.entity_count,
    lp.has_preview, lp.preview_job_state, lp.preview_failure_code,
    coalesce(pj.failure_detail, '')::text AS preview_failure_detail,
    lp.has_production, lp.analysis_job_state, lp.analysis_failure_code,
    coalesce(aj.failure_detail, '')::text AS analysis_failure_detail
FROM blueprints b
JOIN leaf_progress lp ON lp.blueprint_id = b.id
LEFT JOIN phase_jobs pj ON pj.blueprint_id = b.id AND pj.phase = 'preview'
LEFT JOIN phase_jobs aj ON aj.blueprint_id = b.id AND aj.phase = 'analysis'
WHERE b.revision_id = $1
ORDER BY b.position, b.id;

-- name: GetStaffRevisionString :one
-- A revision's original exchange string, whatever the entry's state.
SELECT original_string FROM revisions WHERE id = sqlc.arg(revision_id) AND entry_id = sqlc.arg(entry_id);

-- name: ListStaffEntryReports :many
-- Every report of an entry, open ones first, then newest first.
SELECT r.id, r.reason, r.note, r.revision_number, r.created_at, r.resolved_at, r.resolved_by,
       coalesce(reporter.username, '')::text AS reporter_username
FROM entry_reports r
LEFT JOIN users reporter ON reporter.id = r.reporter_user_id
WHERE r.entry_id = $1
ORDER BY (r.resolved_at IS NULL) DESC, r.created_at DESC, r.id DESC
LIMIT 100;

-- name: ListStaffEntryComments :many
-- An entry's latest comments, tombstones included.
SELECT c.id, c.number, c.body, c.created_at, c.edited_at, c.deleted_by,
       u.username AS author_username,
       (SELECT count(*) FROM comment_reports cr WHERE cr.comment_id = c.id AND cr.resolved_at IS NULL)::bigint
           AS open_reports
FROM comments c
JOIN users u ON u.id = c.author_user_id
WHERE c.entry_id = $1
ORDER BY c.number DESC
LIMIT 50;

-- name: RestoreEntry :one
-- Undoes a soft delete. An expiry that has passed is cleared, so the entry
-- does not expire again at once.
UPDATE entries
SET deleted_at = NULL,
    expires_at = CASE WHEN expires_at <= now() THEN NULL ELSE expires_at END,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NOT NULL
RETURNING *;

-- name: ListLiveEntryLeafIDs :many
-- The current revision's blueprints of a live entry, in tree order (a staff
-- re-run; the dispatchers skip other entries' jobs).
SELECT b.id
FROM entries e
JOIN blueprints b ON b.revision_id = e.current_revision_id
WHERE e.id = $1 AND entry_live(e)
ORDER BY b.position, b.id;
