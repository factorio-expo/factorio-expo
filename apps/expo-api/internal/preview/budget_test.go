package preview

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/previewimage"
	"factorio-expo/apps/expo-api/internal/progress"
	"factorio-expo/apps/expo-api/internal/renderqueue"
	"factorio-expo/apps/expo-api/internal/storage"
)

// fakeRecords is an in-memory preview_files, ordered by write.
type fakeRecords struct {
	mu      sync.Mutex
	rows    map[string]db.RecordPreviewFilesParams
	order   []string
	evicted []string
}

func newFakeRecords() *fakeRecords {
	return &fakeRecords{rows: map[string]db.RecordPreviewFilesParams{}}
}

func (f *fakeRecords) RecordPreviewFiles(_ context.Context, arg db.RecordPreviewFilesParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.rows[arg.SourceSha256]; ok && !arg.Written && old.FullBytes == arg.FullBytes {
		return nil
	}
	f.rows[arg.SourceSha256] = arg
	f.order = append(slices.DeleteFunc(f.order, func(sha string) bool { return sha == arg.SourceSha256 }),
		arg.SourceSha256)
	return nil
}

func (f *fakeRecords) DeletePreviewFilesRecord(_ context.Context, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, sha)
	f.order = slices.DeleteFunc(f.order, func(other string) bool { return other == sha })
	return nil
}

func (f *fakeRecords) PreviewStorageBytes(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := int64(0)
	for _, row := range f.rows {
		total += row.ThumbnailBytes + row.FullBytes.Int64
	}
	return total, nil
}

func (f *fakeRecords) ListEvictablePreviewFiles(
	_ context.Context, limit int32,
) ([]db.ListEvictablePreviewFilesRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rows []db.ListEvictablePreviewFilesRow
	for _, sha := range f.order {
		if row := f.rows[sha]; row.FullBytes.Valid && len(rows) < int(limit) {
			rows = append(rows, db.ListEvictablePreviewFilesRow{SourceSha256: sha, FullBytes: row.FullBytes.Int64})
		}
	}
	return rows, nil
}

func (f *fakeRecords) MarkPreviewFullEvicted(_ context.Context, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.rows[sha]
	row.FullBytes = pgtype.Int8{}
	f.rows[sha] = row
	f.evicted = append(f.evicted, sha)
	return nil
}

// budgetFiles is a Files over a temp directory whose records the test reads.
func budgetFiles(t *testing.T) (*Files, *fakeRecords) {
	t.Helper()
	files := testFiles(t)
	records := newFakeRecords()
	files.records = records
	return files, records
}

// storeSource writes a 900x4 preview's files (full and thumbnail) of the
// given sizes.
func storeSource(t *testing.T, files *Files, sha string, full, thumbnail int) {
	t.Helper()
	encoded := &previewimage.Encoded{Full: make([]byte, full), Thumbnail: make([]byte, thumbnail)}
	if _, err := files.storeAndReference(context.Background(), sha, 900, 4, encoded,
		func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

// Above 90% of the budget the sweep evicts the oldest full files until usage
// is under 80%; thumbnails stay, and a re-render makes a file the newest.
func TestEnforceBudgetEvictsOldestFullFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files, records := budgetFiles(t)
	shas := []string{strings.Repeat("1", 64), strings.Repeat("2", 64), strings.Repeat("3", 64)}
	for _, sha := range shas {
		storeSource(t, files, sha, 100, 10)
	}
	// 330 bytes used, under 90% of 370: nothing to do.
	if err := files.enforceBudget(ctx, records, Budget{MaxBytes: 370}); err != nil || len(records.evicted) != 0 {
		t.Fatalf("under budget: evicted %v, %v", records.evicted, err)
	}
	// Over 90% of 250; 80% is 200, so two full files go (330 -> 130).
	if err := files.enforceBudget(ctx, records, Budget{MaxBytes: 250}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(records.evicted, shas[:2]) {
		t.Fatalf("evicted %v, want the two oldest", records.evicted)
	}
	for i, sha := range shas {
		_, err := files.store.Stat(ctx, fileKey(sha, false))
		if gone := errors.Is(err, storage.ErrNotFound); gone != (i < 2) {
			t.Fatalf("full file %d gone = %v", i, gone)
		}
		if _, err := files.store.Stat(ctx, fileKey(sha, true)); err != nil {
			t.Fatalf("thumbnail %d: %v", i, err)
		}
	}
	// A re-render writes the oldest again: the third is now the oldest full file.
	storeSource(t, files, shas[0], 100, 10)
	records.evicted = nil
	if err := files.enforceBudget(ctx, records, Budget{MaxBytes: 240}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(records.evicted, shas[2:]) {
		t.Fatalf("evicted %v, want the oldest stored", records.evicted)
	}
}

// Low free space evicts until the minimum is back, whatever the budget.
func TestEnforceBudgetRestoresFreeSpace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files, records := budgetFiles(t)
	shas := []string{strings.Repeat("4", 64), strings.Repeat("5", 64)}
	for _, sha := range shas {
		storeSource(t, files, sha, 100, 10)
	}
	budget := Budget{MinFreeBytes: 1000, FreeBytes: func() (int64, error) { return 950, nil }}
	if err := files.enforceBudget(ctx, records, budget); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(records.evicted, shas[:1]) {
		t.Fatalf("evicted %v, want only the oldest", records.evicted)
	}
	// It cannot get back to the minimum: everything goes, then it stops.
	records.evicted = nil
	budget.FreeBytes = func() (int64, error) { return 0, nil }
	if err := files.enforceBudget(ctx, records, budget); err != nil || !slices.Equal(records.evicted, shas[1:]) {
		t.Fatalf("evicted %v, %v", records.evicted, err)
	}
}

// A found file corrects a record that says it was evicted (a crash between
// the delete and the record), so the budget counts it again.
func TestStoreAndReferenceRecordsFoundFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files, records := budgetFiles(t)
	sha := strings.Repeat("7", 64)
	storeSource(t, files, sha, 100, 10)
	if err := records.MarkPreviewFullEvicted(ctx, sha); err != nil {
		t.Fatal(err)
	}
	if _, err := files.storeAndReference(ctx, sha, 900, 4, nil, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if row := records.rows[sha]; row.FullBytes.Int64 != 100 || row.ThumbnailBytes != 10 || row.Written {
		t.Fatalf("record = %+v", row)
	}
}

// fakeJobs is the job tables for Requeuer: an open intent or a failed one
// per blueprint and phase, with the leaf states LeafOf derives from them.
type fakeJobs struct {
	jobs      map[string]string // phase:blueprint -> pending | failed
	keys      map[string]string // phase:blueprint -> queue key
	published []progress.Change
}

func (f *fakeJobs) requeue(phase, blueprintID string, retryFailed bool, queueKey string) (string, error) {
	key := phase + ":" + blueprintID
	if state, ok := f.jobs[key]; ok && (state == "pending" || !retryFailed) {
		return "", pgx.ErrNoRows
	}
	f.jobs[key] = "pending"
	if f.keys == nil {
		f.keys = map[string]string{}
	}
	f.keys[key] = queueKey
	return "entry", nil
}

func (f *fakeJobs) RequeuePhaseJob(_ context.Context, arg db.RequeuePhaseJobParams) (string, error) {
	return f.requeue(arg.Phase, arg.BlueprintID, arg.RetryFailed, arg.QueueKey)
}

func (f *fakeJobs) LockQueueKey(context.Context, string) error { return nil }

func (f *fakeJobs) LastOrderAt(context.Context, string) (pgtype.Timestamptz, error) {
	return pgtype.Timestamptz{}, nil
}

// CountWaitingLeaves counts pending phases queued under queueKey.
func (f *fakeJobs) CountWaitingLeaves(_ context.Context, queueKey string) (int64, error) {
	var count int64
	for key, state := range f.jobs {
		if state == "pending" && f.keys[key] == queueKey {
			count++
		}
	}
	return count, nil
}

// LeafProgress: every phase has a stored result.
func (f *fakeJobs) LeafProgress(_ context.Context, blueprintID string) (progress.Leaf, bool, error) {
	return progress.LeafOf(progress.LeafFacts{
		BlueprintID: blueprintID, HasPreview: true, HasProduction: true,
		PreviewJobState:  f.jobs[progress.PhasePreview+":"+blueprintID],
		AnalysisJobState: f.jobs[progress.PhaseAnalysis+":"+blueprintID],
	}), true, nil
}

// Requeue queues a phase once, tells streams it is pending and wakes the
// dispatcher; a failed intent is reset only when asked.
func TestRequeue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	jobs := &fakeJobs{jobs: map[string]string{}}
	hub := progress.NewHub()
	sub := hub.Subscribe("entry")
	defer sub.Close()
	nudged := 0
	requeuer := NewRequeuer(jobs, jobs, hub, func() { nudged++ })

	ticket := renderqueue.Ticket{Key: "u:visitor", Cap: 1}
	if queued, err := requeuer.Requeue(ctx, "b", progress.PhaseAnalysis, false, ticket); err != nil || !queued || nudged != 1 {
		t.Fatalf("first = %v, %v, %d nudged", queued, err, nudged)
	}
	if key := jobs.keys[progress.PhaseAnalysis+":b"]; key != ticket.Key {
		t.Fatalf("queued under %q", key)
	}
	// The requester's cap: one waiting leaf already.
	if _, err := requeuer.Requeue(ctx, "c", progress.PhaseAnalysis, false, ticket); !errors.Is(err, renderqueue.ErrFull) {
		t.Fatalf("over the cap = %v", err)
	}
	if change := <-sub.Changes(); change.BlueprintID != "b" || change.Phase != progress.PhaseAnalysis ||
		change.State.Status != progress.StatusPending {
		t.Fatalf("published %+v", change)
	}
	if queued, err := requeuer.Requeue(ctx, "b", progress.PhaseAnalysis, true, renderqueue.Staff()); err != nil || queued || nudged != 1 {
		t.Fatalf("open intent = %v, %v", queued, err)
	}
	jobs.jobs[progress.PhaseAnalysis+":b"] = "failed"
	if queued, _ := requeuer.Requeue(ctx, "b", progress.PhaseAnalysis, false, renderqueue.Staff()); queued {
		t.Fatal("failed intent reset without retryFailed")
	}
	if queued, _ := requeuer.Requeue(ctx, "b", progress.PhaseAnalysis, true, renderqueue.Staff()); !queued {
		t.Fatal("failed intent not reset with retryFailed")
	}
	if _, err := requeuer.Requeue(ctx, "b", "other", true, renderqueue.Staff()); err == nil {
		t.Fatal("unknown phase queued")
	}
}

func TestRendersRequestFull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files, _ := budgetFiles(t)
	sha := strings.Repeat("6", 64)
	storeSource(t, files, sha, 100, 10)
	jobs := &fakeJobs{jobs: map[string]string{}}
	renders := NewRenders(files, NewRequeuer(jobs, jobs, nil, nil), jobs)
	request := func(width int) (FullRender, bool) {
		t.Helper()
		state, queued, err := renders.RequestFull(ctx, "b", sha, width, 4, renderqueue.Staff())
		if err != nil {
			t.Fatal(err)
		}
		return state, queued
	}

	if state, queued := request(900); queued || state != (FullRender{Status: progress.StatusReady, FullAvailable: true}) {
		t.Fatalf("stored = %+v, %v", state, queued)
	}
	if err := files.store.Delete(ctx, fileKey(sha, false)); err != nil {
		t.Fatal(err)
	}
	// A small preview never has a full file.
	if state, queued := request(8); queued || !state.FullAvailable {
		t.Fatalf("small = %+v", state)
	}
	if state, queued := request(900); !queued || state != (FullRender{Status: progress.StatusPending}) {
		t.Fatalf("evicted = %+v, %v", state, queued)
	}
	if state, queued := request(900); queued || state.Status != progress.StatusPending {
		t.Fatalf("again = %+v, %v", state, queued)
	}
	// A failed re-render keeps the old preview: ready, without the full file,
	// and not retried from here.
	jobs.jobs[progress.PhasePreview+":b"] = "failed"
	if state, queued := request(900); queued || state != (FullRender{Status: progress.StatusReady}) {
		t.Fatalf("failed = %+v, %v", state, queued)
	}
}
