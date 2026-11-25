package preview

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/storage"
)

// cleanupFiles is a Files over a temp directory that records into the test
// database.
func cleanupFiles(t *testing.T) (*Files, *pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool, queries := sqlTestPool(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewFiles(store, queries), pool, queries
}

// storeCleanupSource writes a 900x4 preview's files (full and thumbnail) for sha
// and records them; referenced also gives a blueprint a previews row of it.
func storeCleanupSource(t *testing.T, files *Files, pool *pgxpool.Pool, sha string, referenced bool) {
	t.Helper()
	ctx := context.Background()
	encoded := &previewimage.Encoded{Full: []byte("full-image"), Thumbnail: []byte("thumb")}
	if _, err := files.storeAndReference(ctx, sha, 900, 4, encoded, func(ctx context.Context) error {
		if !referenced {
			return nil
		}
		_, blueprintID := seedBlueprint(t, pool, true)
		_, err := pool.Exec(ctx, "INSERT INTO previews (blueprint_id, source_sha256, width, height) VALUES ($1, $2, 900, 4)",
			blueprintID, sha)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, files *Files, sha string, thumbnail bool) bool {
	t.Helper()
	_, err := files.store.Stat(context.Background(), fileKey(sha, thumbnail))
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestOrphanedPreviewFiles(t *testing.T) {
	files, pool, queries := cleanupFiles(t)
	ctx := context.Background()
	used, recorded, unrecorded, missing := strings.Repeat("a", 64), strings.Repeat("b", 64),
		strings.Repeat("c", 64), strings.Repeat("d", 64)
	storeCleanupSource(t, files, pool, used, true)
	storeCleanupSource(t, files, pool, recorded, false)
	// Files on disk with no record (a crash before RecordPreviewFiles), and a
	// record whose files are gone.
	if err := files.store.Put(ctx, fileKey(unrecorded, true), strings.NewReader("12345")); err != nil {
		t.Fatal(err)
	}
	if err := queries.RecordPreviewFiles(ctx, db.RecordPreviewFilesParams{SourceSha256: missing, ThumbnailBytes: 3, Written: true}); err != nil {
		t.Fatal(err)
	}
	// Neither a stray file nor a write in progress is a source.
	if err := files.store.Put(ctx, "previews/zz/not-a-preview.webp", strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}

	orphans, err := files.FindOrphans(ctx, queries)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(orphans.Sources)
	if !slices.Equal(orphans.Sources, []string{recorded, unrecorded, missing}) || orphans.Bytes != int64(len("full-image")+len("thumb")+5) {
		t.Fatalf("orphans = %+v", orphans)
	}
	deleted, freed, err := files.DeleteOrphans(ctx, queries)
	if err != nil || deleted != 3 || freed != orphans.Bytes {
		t.Fatalf("delete orphans = %d, %d bytes, %v", deleted, freed, err)
	}
	for _, sha := range []string{recorded, unrecorded} {
		if exists(t, files, sha, true) || exists(t, files, sha, false) {
			t.Fatalf("orphan %s still has files", sha[:4])
		}
	}
	if !exists(t, files, used, true) || !exists(t, files, used, false) {
		t.Fatal("a referenced source lost its files")
	}
	var records int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM preview_files").Scan(&records); err != nil || records != 1 {
		t.Fatalf("records left = %d, %v; want only the referenced one", records, err)
	}
	if orphans, err := files.FindOrphans(ctx, queries); err != nil || len(orphans.Sources) != 0 {
		t.Fatalf("orphans after the cleanup = %+v, %v", orphans, err)
	}
}

func TestEvictTo(t *testing.T) {
	files, pool, queries := cleanupFiles(t)
	ctx := context.Background()
	older, newer := strings.Repeat("e", 64), strings.Repeat("f", 64)
	storeCleanupSource(t, files, pool, older, true)
	storeCleanupSource(t, files, pool, newer, true)
	if _, err := pool.Exec(ctx, "UPDATE preview_files SET stored_at = now() - interval '1 day' WHERE source_sha256 = $1", older); err != nil {
		t.Fatal(err)
	}
	perSource := int64(len("full-image") + len("thumb"))
	target := 2*perSource - 1 // one full file must go

	planned, err := queries.CountEvictionsToTarget(ctx, target)
	if err != nil || planned.Files != 1 || planned.Bytes != int64(len("full-image")) {
		t.Fatalf("dry run = %+v, %v; want the one older full file", planned, err)
	}
	evicted, freed, err := files.EvictTo(ctx, queries, target)
	if err != nil || evicted != 1 || freed != planned.Bytes {
		t.Fatalf("evict = %d, %d bytes, %v", evicted, freed, err)
	}
	if exists(t, files, older, false) || !exists(t, files, older, true) || !exists(t, files, newer, false) {
		t.Fatal("want only the older source's full file evicted, thumbnails kept")
	}
	// Down to zero: every full file goes, thumbnails stay.
	if evicted, _, err := files.EvictTo(ctx, queries, 0); err != nil || evicted != 1 {
		t.Fatalf("evict to 0 = %d, %v; want the last full file", evicted, err)
	}
	if !exists(t, files, newer, true) {
		t.Fatal("evicting removed a thumbnail")
	}
}
