-- name: StorePreview :execrows
-- A preview row is written after its files are stored. Redelivery of a
-- completion must be a no-op, so storing the same source again changes
-- nothing, and a blueprint deleted meanwhile inserts nothing (no foreign-key
-- error, which would abort the caller's transaction). A re-render of an
-- evicted full file that produced a different image (a newer pinned game)
-- points the row at the new source (a trigger queues the old files). Zero
-- rows means the files may be unreferenced (the caller queues them).
INSERT INTO previews (blueprint_id, source_sha256, width, height)
SELECT sqlc.arg(blueprint_id)::short_id, sqlc.arg(source_sha256)::text,
       sqlc.arg(width)::integer, sqlc.arg(height)::integer
WHERE EXISTS (SELECT 1 FROM blueprints WHERE id = sqlc.arg(blueprint_id)::short_id)
ON CONFLICT (blueprint_id) DO UPDATE
SET source_sha256 = EXCLUDED.source_sha256,
    width = EXCLUDED.width,
    height = EXCLUDED.height
WHERE previews.source_sha256 <> EXCLUDED.source_sha256;

-- name: GetPreviewByBlueprint :one
-- full_stored is false once the storage budget evicted the full file (and
-- for small previews, which never have one).
SELECT previews.*, (preview_files.full_bytes IS NOT NULL)::boolean AS full_stored
FROM previews
LEFT JOIN preview_files ON preview_files.source_sha256 = previews.source_sha256
WHERE previews.blueprint_id = $1;

-- name: ListPreviewsByRevision :many
SELECT previews.*, (preview_files.full_bytes IS NOT NULL)::boolean AS full_stored
FROM previews
JOIN blueprints ON blueprints.id = previews.blueprint_id
LEFT JOIN preview_files ON preview_files.source_sha256 = previews.source_sha256
WHERE blueprints.revision_id = $1;

-- name: GetPreviewWithEntryAccess :one
-- A preview image plus the visibility facts of the live entry that owns its
-- blueprint, so the image endpoints enforce the same rules as entry reads.
SELECT previews.source_sha256,
       previews.width,
       previews.height,
       entries.owner_user_id,
       entries.visibility
FROM previews
JOIN blueprints ON blueprints.id = previews.blueprint_id
JOIN revisions ON revisions.id = blueprints.revision_id
JOIN entries ON entries.id = revisions.entry_id
WHERE previews.blueprint_id = $1
  AND entry_live(entries);

-- name: QueuePreviewFileDeletion :exec
-- Files an import stored but no previews row took (its blueprint is gone, or
-- the row already existed); the sweep deletes them if nothing references them.
INSERT INTO preview_file_deletions (source_sha256)
VALUES ($1)
ON CONFLICT (source_sha256) DO NOTHING;

-- name: ListPreviewFileDeletions :many
SELECT source_sha256 FROM preview_file_deletions
ORDER BY created_at
LIMIT $1;

-- name: PreviewSourceReferenced :one
SELECT EXISTS (SELECT 1 FROM previews WHERE source_sha256 = $1);

-- name: DeletePreviewFileDeletion :exec
DELETE FROM preview_file_deletions WHERE source_sha256 = $1;

-- name: RecordPreviewFiles :exec
-- Records the files of a source that preview.Files wrote (written: they count
-- as new for the budget) or found on disk. Found files only correct the row
-- when it disagrees (a full file recorded as evicted is there after all).
INSERT INTO preview_files (source_sha256, thumbnail_bytes, full_bytes)
VALUES (sqlc.arg(source_sha256), sqlc.arg(thumbnail_bytes), sqlc.narg(full_bytes))
ON CONFLICT (source_sha256) DO UPDATE
SET thumbnail_bytes = EXCLUDED.thumbnail_bytes,
    full_bytes = EXCLUDED.full_bytes,
    stored_at = now()
WHERE sqlc.arg(written)::boolean
   OR preview_files.full_bytes IS DISTINCT FROM EXCLUDED.full_bytes;

-- name: DeletePreviewFilesRecord :exec
DELETE FROM preview_files WHERE source_sha256 = $1;

-- name: PreviewStorageBytes :one
-- Bytes of every stored preview file, as the budget counts them.
SELECT COALESCE(sum(thumbnail_bytes + COALESCE(full_bytes, 0)), 0)::bigint
FROM preview_files;

-- name: ListEvictablePreviewFiles :many
-- Full files in eviction order: the oldest written first.
SELECT source_sha256, full_bytes::bigint AS full_bytes
FROM preview_files
WHERE full_bytes IS NOT NULL
ORDER BY stored_at, source_sha256
LIMIT $1;

-- name: MarkPreviewFullEvicted :exec
UPDATE preview_files SET full_bytes = NULL WHERE source_sha256 = $1;
