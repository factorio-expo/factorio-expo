package preview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/internal/shortid"
)

// testLockID is the advisory-lock key every database-backed test binary shares
// (see catalog_test.go): packages truncate the catalog tables, so they take
// turns.
const testLockID = 8175021

// TestMain serializes this binary against the other database-backed packages
// when TEST_DATABASE_URL is set; otherwise the SQL tests below skip.
func TestMain(m *testing.M) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	lock, err := pool.Acquire(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquire: %v\n", err)
		os.Exit(1)
	}
	if _, err := lock.Exec(ctx, "SELECT pg_advisory_lock($1)", testLockID); err != nil {
		fmt.Fprintf(os.Stderr, "lock: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	_, _ = lock.Exec(ctx, "SELECT pg_advisory_unlock($1)", testLockID)
	lock.Release()
	pool.Close()
	os.Exit(code)
}

func sqlTestPool(t *testing.T) (*pgxpool.Pool, *db.Queries) {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(context.Background(),
		"TRUNCATE entries, revisions, books, blueprints, previews, phase_jobs, entry_search_jobs, preview_file_deletions, preview_files CASCADE",
	); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool, db.New(pool)
}

// seedBlueprint inserts a live entry with one revision holding one blueprint
// and returns the entry and blueprint ids. When current is false the revision
// is not the entry's current revision.
func seedBlueprint(t *testing.T, pool *pgxpool.Pool, current bool) (entryID, blueprintID string) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, "INSERT INTO entries (id) VALUES ($1) RETURNING id", shortid.New()).Scan(&entryID); err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	var revisionID string
	if err := pool.QueryRow(ctx,
		"INSERT INTO revisions (id, entry_id, version_number, original_string, tree) VALUES ($1, $2, 1, 'x', '{}') RETURNING id",
		shortid.New(), entryID).Scan(&revisionID); err != nil {
		t.Fatalf("insert revision: %v", err)
	}
	if current {
		if _, err := pool.Exec(ctx, "UPDATE entries SET current_revision_id = $2 WHERE id = $1", entryID, revisionID); err != nil {
			t.Fatalf("set current revision: %v", err)
		}
	}
	if err := pool.QueryRow(ctx,
		"INSERT INTO blueprints (id, revision_id, exchange_string, exchange_sha256) VALUES ($1, $2, 'bp', sha256(gen_random_uuid()::text::bytea)) RETURNING id",
		shortid.New(), revisionID).Scan(&blueprintID); err != nil {
		t.Fatalf("insert blueprint: %v", err)
	}
	return entryID, blueprintID
}

// previewIntent and analysisIntent queue a phase now under the system key.
func previewIntent(blueprintID string) db.EnqueuePhaseJobParams {
	return db.EnqueuePhaseJobParams{BlueprintID: blueprintID, Phase: slotPreview, QueueKey: renderqueue.SystemKey, OrderAt: now()}
}

func analysisIntent(blueprintID string) db.EnqueuePhaseJobParams {
	return db.EnqueuePhaseJobParams{BlueprintID: blueprintID, Phase: slotAnalysis, QueueKey: renderqueue.SystemKey, OrderAt: now()}
}

func now() pgtype.Timestamptz { return pgtype.Timestamptz{Time: time.Now(), Valid: true} }

// claim claims with a window no test fills (window tests use claimWindow).
func claim(t *testing.T, queries *db.Queries, lease time.Duration) []db.ClaimDispatchJobsRow {
	t.Helper()
	return claimWindow(t, queries, lease, 1000)
}

func claimWindow(t *testing.T, queries *db.Queries, lease time.Duration, window int64) []db.ClaimDispatchJobsRow {
	t.Helper()
	rows, err := queries.ClaimDispatchJobs(context.Background(), db.ClaimDispatchJobsParams{
		LeaseSeconds: lease.Seconds(), BatchSize: 50, DispatchWindow: window,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return rows
}

func TestClaimLeasesRowsOfLiveEntries(t *testing.T) {
	pool, queries := sqlTestPool(t)
	_, blueprintID := seedBlueprint(t, pool, true)
	ctx := context.Background()
	_ = queries.EnqueuePhaseJob(ctx, previewIntent(blueprintID))
	_ = queries.EnqueuePhaseJob(ctx, analysisIntent(blueprintID))

	first := claim(t, queries, time.Minute)
	if len(first) != 1 || !first[0].PreviewClaimed || !first[0].AnalysisClaimed ||
		first[0].PreviewState.String != "pending" || first[0].ExchangeString != "bp" {
		t.Fatalf("first claim = %+v", first)
	}
	if second := claim(t, queries, time.Minute); len(second) != 0 {
		t.Fatalf("leased row claimed again: %+v", second)
	}
	if _, err := pool.Exec(ctx, "UPDATE phase_jobs SET available_at = now() - interval '1 second' WHERE phase = 'preview'"); err != nil {
		t.Fatal(err)
	}
	third := claim(t, queries, time.Minute)
	if len(third) != 1 || !third[0].PreviewClaimed || third[0].AnalysisClaimed {
		t.Fatalf("expired lease must reclaim only the expired phase: %+v", third)
	}
}

func TestConcurrentClaimsNeverShareARow(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	for range 20 {
		_, blueprintID := seedBlueprint(t, pool, true)
		_ = queries.EnqueuePhaseJob(ctx, previewIntent(blueprintID))
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			rows, err := queries.ClaimDispatchJobs(ctx, db.ClaimDispatchJobsParams{LeaseSeconds: 60, BatchSize: 5, DispatchWindow: 1000})
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, row := range rows {
				seen[row.BlueprintID]++
			}
		})
	}
	wg.Wait()
	for id, claims := range seen {
		if claims != 1 {
			t.Fatalf("blueprint %s claimed %d times", id, claims)
		}
	}
	if len(seen) != 20 {
		t.Fatalf("claimed %d of 20 rows", len(seen))
	}
}

func TestDeletedEntriesAreNeverClaimed(t *testing.T) {
	pool, queries := sqlTestPool(t)
	entryID, blueprintID := seedBlueprint(t, pool, true)
	ctx := context.Background()
	_ = queries.EnqueuePhaseJob(ctx, previewIntent(blueprintID))
	_ = queries.EnqueuePhaseJob(ctx, analysisIntent(blueprintID))
	if _, err := pool.Exec(ctx, "UPDATE entries SET deleted_at = now() WHERE id = $1", entryID); err != nil {
		t.Fatal(err)
	}
	if rows := claim(t, queries, time.Minute); len(rows) != 0 {
		t.Fatalf("deleted entry claimed: %+v", rows)
	}
}

func TestReconcileOnlyTouchesLiveCurrentRevisions(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	_, currentBP := seedBlueprint(t, pool, true)
	_, oldBP := seedBlueprint(t, pool, false)
	deletedEntry, deletedBP := seedBlueprint(t, pool, true)
	if _, err := pool.Exec(ctx, "UPDATE entries SET deleted_at = now() WHERE id = $1", deletedEntry); err != nil {
		t.Fatal(err)
	}
	if err := queries.ReconcileMissingPreviewJobs(ctx, now()); err != nil {
		t.Fatal(err)
	}
	for blueprint, want := range map[string]bool{currentBP: true, oldBP: false, deletedBP: false} {
		var exists bool
		if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM phase_jobs WHERE phase = 'preview' AND blueprint_id = $1)", blueprint).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Fatalf("preview job exists = %v, want %v", exists, want)
		}
	}
}

func TestReEnqueuedSearchJobGetsFreshBudget(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	entryID, _ := seedBlueprint(t, pool, true)
	if _, err := pool.Exec(ctx,
		`INSERT INTO entry_search_jobs (entry_id, state, retry_count, failure_code, failure_detail)
		 VALUES ($1, 'failed', 5, 'sync_failed', 'down')`, entryID); err != nil {
		t.Fatal(err)
	}
	if err := queries.EnqueueSearchIndexJob(ctx, entryID); err != nil {
		t.Fatal(err)
	}
	var state, code string
	var retries int32
	if err := pool.QueryRow(ctx,
		"SELECT state, retry_count, failure_code FROM entry_search_jobs WHERE entry_id = $1", entryID,
	).Scan(&state, &retries, &code); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || retries != 0 || code != "" {
		t.Fatalf("re-enqueued job = %s / %d / %q", state, retries, code)
	}
	if err := queries.EnqueueSearchDeleteJob(ctx, entryID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE entry_search_jobs SET retry_count = 4"); err != nil {
		t.Fatal(err)
	}
	if err := queries.EnqueueSearchDeleteJob(ctx, entryID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT retry_count FROM entry_search_jobs WHERE entry_id = $1", entryID).Scan(&retries); err != nil || retries != 0 {
		t.Fatalf("delete re-enqueue retry_count = %d (%v)", retries, err)
	}
}

func TestSearchClaimLeasesAndFailedAnalysisKeepsDetail(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	entryID, blueprintID := seedBlueprint(t, pool, true)
	if err := queries.EnqueueSearchIndexJob(ctx, entryID); err != nil {
		t.Fatal(err)
	}
	args := db.ClaimSearchJobsParams{LeaseSeconds: 60, BatchSize: 10}
	if rows, err := queries.ClaimSearchJobs(ctx, args); err != nil || len(rows) != 1 {
		t.Fatalf("first search claim = %v, %v", rows, err)
	}
	if rows, err := queries.ClaimSearchJobs(ctx, args); err != nil || len(rows) != 0 {
		t.Fatalf("leased search row claimed again = %v, %v", rows, err)
	}

	_ = queries.EnqueuePhaseJob(ctx, analysisIntent(blueprintID))
	if err := queries.MarkPhaseJobFailed(ctx, db.MarkPhaseJobFailedParams{
		Phase:       slotAnalysis,
		BlueprintID: blueprintID, FailureCode: "analysis_failed", FailureDetail: "calculation exploded",
	}); err != nil {
		t.Fatal(err)
	}
	var state, detail string
	if err := pool.QueryRow(ctx, "SELECT state, failure_detail FROM phase_jobs WHERE phase = 'analysis' AND blueprint_id = $1",
		blueprintID).Scan(&state, &detail); err != nil || state != "failed" || detail != "calculation exploded" {
		t.Fatalf("analysis job = %q %q, %v", state, detail, err)
	}
}

func TestDeletedEntryIsNotReindexed(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	entryID, blueprintID := seedBlueprint(t, pool, true)

	if _, err := queries.GetEntryIDByBlueprint(ctx, blueprintID); err != nil {
		t.Fatalf("live entry lookup: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE entries SET deleted_at = now() WHERE id = $1", entryID); err != nil {
		t.Fatal(err)
	}
	if err := queries.EnqueueSearchDeleteJob(ctx, entryID); err != nil {
		t.Fatal(err)
	}

	// A late import resolves the entry through its blueprint: a deleted entry
	// must not resolve, and even a direct enqueue must keep the delete job.
	if _, err := queries.GetEntryIDByBlueprint(ctx, blueprintID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deleted entry lookup = %v, want no rows", err)
	}
	if err := queries.EnqueueSearchIndexJob(ctx, entryID); err != nil {
		t.Fatal(err)
	}
	var action string
	if err := pool.QueryRow(ctx, "SELECT action FROM entry_search_jobs WHERE entry_id = $1", entryID).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "delete" {
		t.Fatalf("search job action = %q, want delete", action)
	}
}

// Deleting a previews row, also through an entry's cascade, queues its files;
// the sweep keeps files another row still references and deletes the rest.
func TestDeletedPreviewFilesAreSweptOnceUnreferenced(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	files := testFiles(t)
	sha := strings.Repeat("c", 64)
	for _, thumbnail := range []bool{false, true} {
		if err := files.store.Put(ctx, fileKey(sha, thumbnail), strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	var entries []string
	for range 2 {
		entryID, blueprintID := seedBlueprint(t, pool, true)
		if _, err := queries.StorePreview(ctx, db.StorePreviewParams{
			BlueprintID: blueprintID, SourceSha256: sha, Width: 4, Height: 2,
		}); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entryID)
	}
	stored := func() bool {
		_, err := files.store.Stat(ctx, fileKey(sha, false))
		return err == nil
	}

	for i, entryID := range entries {
		if _, err := pool.Exec(ctx, "DELETE FROM entries WHERE id = $1", entryID); err != nil {
			t.Fatal(err)
		}
		queued, err := queries.ListPreviewFileDeletions(ctx, 10)
		if err != nil || len(queued) != 1 || queued[0] != sha {
			t.Fatalf("after delete %d: queued = %v, %v", i, queued, err)
		}
		if err := files.sweep(ctx, queries); err != nil {
			t.Fatal(err)
		}
		if last := i == len(entries)-1; stored() == last {
			t.Fatalf("after delete %d: stored = %v", i, stored())
		}
		if queued, _ := queries.ListPreviewFileDeletions(ctx, 10); len(queued) != 0 {
			t.Fatalf("sweep left %v queued", queued)
		}
	}
}

// The budget against the real tables: stored files are recorded, eviction
// clears full_stored, a re-render request queues a preview-only intent, and a
// re-render with a different image points the row at it and queues the old
// files.
func TestEvictionAndReRenderAgainstTheDatabase(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	files := NewFiles(testFiles(t).store, queries)
	_, blueprintID := seedBlueprint(t, pool, true)
	sha, newer := strings.Repeat("e", 64), strings.Repeat("f", 64)
	encoded := &previewimage.Encoded{Full: make([]byte, 1000), Thumbnail: make([]byte, 10)}
	reference := func(sha string) func(context.Context) error {
		return func(ctx context.Context) error {
			_, err := queries.StorePreview(ctx, db.StorePreviewParams{
				BlueprintID: blueprintID, SourceSha256: sha, Width: 900, Height: 4,
			})
			return err
		}
	}
	if _, err := files.storeAndReference(ctx, sha, 900, 4, encoded, reference(sha)); err != nil {
		t.Fatal(err)
	}
	fullStored := func() bool {
		t.Helper()
		row, err := queries.GetPreviewByBlueprint(ctx, blueprintID)
		if err != nil {
			t.Fatal(err)
		}
		return row.FullStored
	}
	if used, err := queries.PreviewStorageBytes(ctx); err != nil || used != 1010 || !fullStored() {
		t.Fatalf("stored: used = %d, %v, full stored %v", used, err, fullStored())
	}

	if err := files.enforceBudget(ctx, queries, Budget{MaxBytes: 1000}); err != nil {
		t.Fatal(err)
	}
	if used, err := queries.PreviewStorageBytes(ctx); err != nil || used != 10 || fullStored() {
		t.Fatalf("evicted: used = %d, %v, full stored %v", used, err, fullStored())
	}

	leaves := &fakeJobs{jobs: map[string]string{}}
	renders := NewRenders(files, NewRequeuer(queries, leaves, nil, nil), leaves)
	if _, queued, err := renders.RequestFull(ctx, blueprintID, sha, 900, 4, renderqueue.Staff()); err != nil || !queued {
		t.Fatalf("render = %v, %v", queued, err)
	}
	if _, queued, err := renders.RequestFull(ctx, blueprintID, sha, 900, 4, renderqueue.Staff()); err != nil || queued {
		t.Fatalf("second render = %v, %v", queued, err)
	}
	rows := claim(t, queries, time.Minute)
	if len(rows) != 1 || !rows[0].PreviewClaimed || rows[0].AnalysisClaimed {
		t.Fatalf("claimed %+v, want a preview-only intent", rows)
	}
	// A failed intent is reset only with retryFailed (an admin re-run).
	if err := queries.MarkPhaseJobFailed(ctx, db.MarkPhaseJobFailedParams{
		Phase:       slotPreview,
		BlueprintID: blueprintID, FailureCode: "x",
	}); err != nil {
		t.Fatal(err)
	}
	requeue := func(retryFailed bool) error {
		_, err := queries.RequeuePhaseJob(ctx, db.RequeuePhaseJobParams{
			Phase:       slotPreview,
			BlueprintID: blueprintID, RetryFailed: retryFailed, QueueKey: renderqueue.StaffKey, OrderAt: now(),
		})
		return err
	}
	if err := requeue(false); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("failed intent without retry: %v", err)
	}
	if err := requeue(true); err != nil {
		t.Fatalf("failed intent with retry: %v", err)
	}
	if rows := claim(t, queries, time.Minute); len(rows) != 1 || rows[0].PreviewRetryCount.Int32 != 0 {
		t.Fatalf("reset intent claimed %+v", rows)
	}

	// The re-render produced a newer image.
	if _, err := files.storeAndReference(ctx, newer, 900, 4, encoded, reference(newer)); err != nil {
		t.Fatal(err)
	}
	row, err := queries.GetPreviewByBlueprint(ctx, blueprintID)
	if err != nil || row.SourceSha256 != newer || !row.FullStored {
		t.Fatalf("after re-render = %+v, %v", row, err)
	}
	if queued, err := queries.ListPreviewFileDeletions(ctx, 10); err != nil || len(queued) != 1 || queued[0] != sha {
		t.Fatalf("queued = %v, %v, want the replaced source", queued, err)
	}
	if err := files.sweep(ctx, queries); err != nil {
		t.Fatal(err)
	}
	if used, err := queries.PreviewStorageBytes(ctx); err != nil || used != 1010 {
		t.Fatalf("after sweep: used = %d, %v", used, err)
	}
}
