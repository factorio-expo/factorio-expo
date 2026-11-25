-- name: AddEntryViews :exec
-- The view counter's periodic flush: adds the counted views of many entries
-- at once, to the all-time count and to today's (UTC) row. Only live,
-- non-private entries that are not quick previews are counted; ids that name
-- no such entry are dropped here.
WITH counted AS (
    SELECT entries.id AS entry_id, added.views
    FROM (SELECT unnest(sqlc.arg(entry_ids)::text[]) AS entry_id,
                 unnest(sqlc.arg(views)::bigint[]) AS views) AS added
    JOIN entries ON entries.id = added.entry_id
    WHERE entry_lasting(entries)
      AND entries.visibility <> 'private'
      AND added.views > 0
), daily AS (
    INSERT INTO entry_view_days (day, entry_id, views)
    SELECT (now() AT TIME ZONE 'UTC')::date, entry_id, views FROM counted
    ON CONFLICT (day, entry_id) DO UPDATE
        SET views = entry_view_days.views + excluded.views
)
INSERT INTO entry_view_counts (entry_id, views)
SELECT entry_id, views FROM counted
ON CONFLICT (entry_id) DO UPDATE
    SET views = entry_view_counts.views + excluded.views;

-- name: ListTrendingEntryIDs :many
-- The listed entries with the most views in the period: today, this week
-- (since Monday) or this month, all UTC; cards via ListEntryCards.
SELECT entries.id
FROM entry_view_days
JOIN entries ON entries.id = entry_view_days.entry_id
WHERE entry_view_days.day >= CASE sqlc.arg(period)::text
          WHEN 'day' THEN (now() AT TIME ZONE 'UTC')::date
          WHEN 'week' THEN date_trunc('week', now() AT TIME ZONE 'UTC')::date
          ELSE date_trunc('month', now() AT TIME ZONE 'UTC')::date
      END
  AND entry_listed(entries)
GROUP BY entries.id
ORDER BY sum(entry_view_days.views) DESC, entries.id
LIMIT sqlc.arg(row_limit);

-- name: DeleteOldEntryViewDays :execrows
-- Days before this month are never read.
DELETE FROM entry_view_days
WHERE day < date_trunc('month', now() AT TIME ZONE 'UTC')::date;

-- name: GetEntryViewCount :one
-- An entry's all-time views; 0 when none were counted yet.
SELECT coalesce((SELECT views FROM entry_view_counts WHERE entry_id = $1), 0)::bigint;

-- name: ListUnindexedViewCounts :many
-- View counts the search index has not seen yet. searchable says whether the
-- entry has a search document at all; the others are only marked indexed (an
-- entry that becomes searchable later is projected with its current count).
SELECT entry_view_counts.entry_id,
       entry_view_counts.views,
       entry_listed(entries)::boolean AS searchable
FROM entry_view_counts
JOIN entries ON entries.id = entry_view_counts.entry_id
WHERE entry_view_counts.views <> entry_view_counts.indexed_views
ORDER BY entry_view_counts.entry_id
LIMIT $1;

-- name: MarkViewCountsIndexed :exec
-- Records the counts the search refresh sent.
UPDATE entry_view_counts
SET indexed_views = sent.views
FROM (SELECT unnest(sqlc.arg(entry_ids)::text[]) AS entry_id,
             unnest(sqlc.arg(views)::bigint[]) AS views) AS sent
WHERE entry_view_counts.entry_id = sent.entry_id;
