package catalog_test

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

var (
	addressA = []byte("address-a-hash-0123456789abcdef!")
	addressB = []byte("address-b-hash-0123456789abcdef!")
)

func uploadFrom(t *testing.T, service *catalog.Service, fixture string, ipHash []byte) catalog.Entry {
	t.Helper()
	entry, err := service.CreateEntry(context.Background(), catalog.Submission{
		Exchange: readFixture(t, fixture), IPHash: ipHash,
	})
	if err != nil {
		t.Fatalf("upload %s: %v", fixture, err)
	}
	return entry
}

// entryLeaves are the live revision's blueprint ids in tree order.
func entryLeaves(t *testing.T, pool *pgxpool.Pool, entryID string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT b.id FROM blueprints b
		JOIN entries e ON e.current_revision_id = b.revision_id
		WHERE e.id = $1 ORDER BY b.position`, entryID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// storeResult finishes a leaf as the dispatcher would: a preview (whose full
// file is stored or not) and a current production snapshot, intents gone.
func storeResult(t *testing.T, pool *pgxpool.Pool, blueprintID, sha string, fullStored bool) {
	t.Helper()
	ctx := context.Background()
	full := any(nil)
	if fullStored {
		full = int64(100)
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO preview_files (source_sha256, thumbnail_bytes, full_bytes) VALUES ($1, 10, $2)
		  ON CONFLICT (source_sha256) DO UPDATE SET full_bytes = EXCLUDED.full_bytes`, []any{sha, full}},
		{`INSERT INTO previews (blueprint_id, source_sha256, width, height) VALUES ($1, $2, 900, 4)`, []any{blueprintID, sha}},
		{`DELETE FROM phase_jobs WHERE phase = 'preview' AND blueprint_id = $1`, []any{blueprintID}},
		{`UPDATE blueprint_stats SET production = '{"rates": 1}', production_schema_version = 1,
		  production_state = 'complete', production_stale = false, production_calculated_at = now()
		  WHERE blueprint_id = $1`, []any{blueprintID}},
		{`DELETE FROM phase_jobs WHERE phase = 'analysis' AND blueprint_id = $1`, []any{blueprintID}},
	} {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("store result: %v", err)
		}
	}
}

// failLeaf fails both phases of a leaf for good with code.
func failLeaf(t *testing.T, pool *pgxpool.Pool, blueprintID, code string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"UPDATE phase_jobs SET state = 'failed', failure_code = $2, failure_detail = 'detail' WHERE blueprint_id = $1",
		blueprintID, code); err != nil {
		t.Fatal(err)
	}
}

// jobStates are a leaf's intents: "" when it has none.
func jobStates(t *testing.T, pool *pgxpool.Pool, blueprintID string) (preview, analysis string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE((SELECT state || ':' || failure_code FROM phase_jobs WHERE blueprint_id = $1 AND phase = 'preview'), ''),
		       COALESCE((SELECT state || ':' || failure_code FROM phase_jobs WHERE blueprint_id = $1 AND phase = 'analysis'), '')`,
		blueprintID).Scan(&preview, &analysis); err != nil {
		t.Fatal(err)
	}
	return preview, analysis
}

// An upload takes the stored preview and analysis of any leaf with the same
// string and queues nothing for it.
func TestUploadReusesStoredResults(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	first := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	source := entryLeaves(t, pool, first.ID)[0]
	var hashMatches bool
	if err := pool.QueryRow(ctx,
		"SELECT exchange_sha256 = sha256(convert_to(exchange_string, 'UTF8')) FROM blueprints WHERE id = $1",
		source).Scan(&hashMatches); err != nil || !hashMatches {
		t.Fatalf("exchange_sha256 is the string's SHA-256: %v, %v", hashMatches, err)
	}
	sha := strings.Repeat("a", 64)
	storeResult(t, pool, source, sha, true)

	second := uploadFrom(t, service, "bp-starter-smelter.txt", addressB)
	leaf := entryLeaves(t, pool, second.ID)[0]
	if preview, analysis := jobStates(t, pool, leaf); preview != "" || analysis != "" {
		t.Fatalf("reused leaf still queued: %q, %q", preview, analysis)
	}
	var gotSHA, state string
	var version int32
	if err := pool.QueryRow(ctx, `
		SELECT p.source_sha256, bs.production_state, bs.production_schema_version
		FROM previews p JOIN blueprint_stats bs ON bs.blueprint_id = p.blueprint_id
		WHERE p.blueprint_id = $1`, leaf).Scan(&gotSHA, &state, &version); err != nil {
		t.Fatalf("reused result: %v", err)
	}
	if gotSHA != sha || state != "complete" || version != 1 {
		t.Fatalf("reused = %s %s %d", gotSHA, state, version)
	}
	if got, err := service.GetEntry(ctx, second.ID, ""); err != nil || got.Revision.Root.Preview == nil {
		t.Fatalf("reused entry = %+v, %v", got.Revision.Root, err)
	}
}

// A preview whose full file was evicted is not reused (the leaf renders and
// writes it again); its analysis still is.
func TestEvictedPreviewIsNotReused(t *testing.T) {
	service, pool := newService(t)
	first := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	storeResult(t, pool, entryLeaves(t, pool, first.ID)[0], strings.Repeat("b", 64), false)

	second := uploadFrom(t, service, "bp-starter-smelter.txt", addressB)
	preview, analysis := jobStates(t, pool, entryLeaves(t, pool, second.ID)[0])
	if preview != "pending:" || analysis != "" {
		t.Fatalf("states = %q, %q; want the preview queued and the analysis reused", preview, analysis)
	}
}

// Final failures that are the blueprint's own are copied (a quick preview is
// refused outright); retryable ones are not.
func TestUploadReusesFinalFailuresOnly(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	crashed := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	failLeaf(t, pool, entryLeaves(t, pool, crashed.ID)[0], "factorio_restarted")

	again := uploadFrom(t, service, "bp-starter-smelter.txt", addressB)
	preview, analysis := jobStates(t, pool, entryLeaves(t, pool, again.ID)[0])
	if preview != "failed:factorio_restarted" || analysis != "failed:factorio_restarted" {
		t.Fatalf("states = %q, %q", preview, analysis)
	}
	var failed *catalog.PreviewFailedError
	if _, err := service.CreateQuickPreview(ctx, readFixture(t, "bp-starter-smelter.txt"), "", addressB); !errors.As(err, &failed) ||
		failed.Code != "factorio_restarted" {
		t.Fatalf("quick preview of a crashing string = %v", err)
	}
	var previews int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM entries WHERE quick_preview").Scan(&previews); err != nil || previews != 0 {
		t.Fatalf("refused quick preview stored: %d, %v", previews, err)
	}

	timedOut := uploadFrom(t, service, "bp-green-circuit-block.txt", addressA)
	failLeaf(t, pool, entryLeaves(t, pool, timedOut.ID)[0], "capture_timeout")
	retried := uploadFrom(t, service, "bp-green-circuit-block.txt", addressB)
	if preview, analysis := jobStates(t, pool, entryLeaves(t, pool, retried.ID)[0]); preview != "pending:" || analysis != "pending:" {
		t.Fatalf("retryable failure copied: %q, %q", preview, analysis)
	}
}

func capped(t *testing.T, anonymous int) (*catalog.Service, *pgxpool.Pool) {
	t.Helper()
	policy := renderqueue.Defaults()
	policy.CapAnonymous = anonymous
	return newServiceWith(t, catalog.Options{RenderQueue: policy})
}

// The cap counts an uploader's waiting leaves, refuses a whole upload that
// would pass it, and ignores leaves that took a stored result.
func TestRenderQueueCap(t *testing.T) {
	service, pool := capped(t, 2)
	ctx := context.Background()
	var entries int
	count := func() int {
		t.Helper()
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM entries").Scan(&entries); err != nil {
			t.Fatal(err)
		}
		return entries
	}
	if _, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "book-starter-kit.txt"), IPHash: addressA,
	}); !errors.Is(err, renderqueue.ErrFull) || count() != 0 {
		t.Fatalf("book over the cap = %v, %d entries", err, entries)
	}
	single := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	qualityEntry := uploadFrom(t, service, "bp-green-circuit-block.txt", addressA)
	if _, err := service.CreateEntry(ctx, catalog.Submission{
		Exchange: readFixture(t, "art-planet-mosaic.txt"), IPHash: addressA,
	}); !errors.Is(err, renderqueue.ErrFull) {
		t.Fatalf("third leaf = %v", err)
	}
	// Another address has its own cap.
	uploadFrom(t, service, "art-planet-mosaic.txt", addressB)

	// A finished leaf frees its place; a fully reused upload needs none.
	storeResult(t, pool, entryLeaves(t, pool, single.ID)[0], strings.Repeat("c", 64), true)
	uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	uploadFrom(t, service, "ship-hauler.txt", addressA)

	var key string
	var delay time.Duration
	quality := entryLeaves(t, pool, qualityEntry.ID)[0]
	if err := pool.QueryRow(ctx, "SELECT queue_key, order_at - created_at FROM phase_jobs WHERE phase = 'preview' AND blueprint_id = $1",
		quality).Scan(&key, &delay); err != nil {
		t.Fatal(err)
	}
	// The address's second waiting leaf: the anonymous delay plus one gap.
	if want := 2*time.Minute + 30*time.Second; key != "ip:"+hex.EncodeToString(addressA) ||
		delay < want-time.Second || delay > want+time.Second {
		t.Fatalf("anonymous intent = %q, +%s", key, delay)
	}
}

// Two uploads of one uploader at once cannot both slip under the cap.
func TestConcurrentUploadsRespectTheCap(t *testing.T) {
	service, _ := capped(t, 1)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, fixture := range []string{"bp-starter-smelter.txt", "bp-green-circuit-block.txt"} {
		wg.Go(func() {
			_, err := service.CreateEntry(context.Background(), catalog.Submission{
				Exchange: readFixture(t, fixture), IPHash: addressA,
			})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	var accepted, full int
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, renderqueue.ErrFull):
			full++
		default:
			t.Fatal(err)
		}
	}
	if accepted != 1 || full != 1 {
		t.Fatalf("accepted %d, refused %d", accepted, full)
	}
}

// Leaves that waited for an identical one take its result once stored, or
// its final failure.
func TestReuseWaitingResults(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	first := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	second := uploadFrom(t, service, "bp-starter-smelter.txt", addressB)
	crashing := uploadFrom(t, service, "bp-green-circuit-block.txt", addressA)
	waiting := uploadFrom(t, service, "bp-green-circuit-block.txt", addressB)
	if reused, err := service.ReuseWaitingResults(ctx); err != nil || len(reused) != 0 {
		t.Fatalf("nothing stored yet = %+v, %v", reused, err)
	}
	storeResult(t, pool, entryLeaves(t, pool, first.ID)[0], strings.Repeat("d", 64), true)
	failLeaf(t, pool, entryLeaves(t, pool, crashing.ID)[0], "invalid_blueprint")

	reused, err := service.ReuseWaitingResults(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leaf := entryLeaves(t, pool, second.ID)[0]
	failed := entryLeaves(t, pool, waiting.ID)[0]
	want := map[renderqueue.ReusedLeaf]bool{
		{EntryID: second.ID, BlueprintID: leaf, Phase: progress.PhasePreview}:                                       true,
		{EntryID: second.ID, BlueprintID: leaf, Phase: progress.PhaseAnalysis}:                                      true,
		{EntryID: waiting.ID, BlueprintID: failed, Phase: progress.PhasePreview, FailureCode: "invalid_blueprint"}:  true,
		{EntryID: waiting.ID, BlueprintID: failed, Phase: progress.PhaseAnalysis, FailureCode: "invalid_blueprint"}: true,
	}
	if len(reused) != len(want) {
		t.Fatalf("reused = %+v", reused)
	}
	for _, got := range reused {
		if !want[got] {
			t.Fatalf("unexpected reuse %+v", got)
		}
	}
	if preview, analysis := jobStates(t, pool, leaf); preview != "" || analysis != "" {
		t.Fatalf("reused leaf still queued: %q, %q", preview, analysis)
	}
}

// Reusing a preview locks the source row until commit, so a delete of the
// source (which would leave the shared files unreferenced for the sweep)
// waits for the new reference.
func TestReuseLocksTheSourcePreview(t *testing.T) {
	service, pool := newService(t)
	ctx := context.Background()
	first := uploadFrom(t, service, "bp-starter-smelter.txt", addressA)
	uploadFrom(t, service, "bp-starter-smelter.txt", addressB)
	source := entryLeaves(t, pool, first.ID)[0]
	storeResult(t, pool, source, strings.Repeat("e", 64), true)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	reused, err := db.New(tx).ReusePreviews(ctx, db.ReusePreviewsParams{ThumbnailMax: 800})
	if err != nil || len(reused) != 1 {
		t.Fatalf("reuse = %+v, %v", reused, err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET lock_timeout = '200ms'"); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, "DELETE FROM previews WHERE blueprint_id = $1", source)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("delete of the source during reuse = %v, want a lock timeout", err)
	}
	if _, err := conn.Exec(ctx, "RESET lock_timeout"); err != nil {
		t.Fatal(err)
	}
}
