package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

// fakeJobs holds failed jobs by kind and target and plays every dependency.
type fakeJobs struct {
	failed map[string]db.ListFailedJobsRow
	// leaves are live entries' current blueprints; open are phases queued.
	leaves   map[string][]string
	open     map[string]bool
	requeued []string
	nudged   int
	audits   []audit.Record
}

func newFake(rows ...db.ListFailedJobsRow) *fakeJobs {
	f := &fakeJobs{failed: map[string]db.ListFailedJobsRow{}, leaves: map[string][]string{}, open: map[string]bool{}}
	for _, row := range rows {
		f.failed[row.Kind+"/"+row.TargetID] = row
	}
	return f
}

func (f *fakeJobs) ListFailedJobs(_ context.Context, arg db.ListFailedJobsParams) ([]db.ListFailedJobsRow, error) {
	rows := []db.ListFailedJobsRow{}
	for _, row := range f.failed {
		if (!arg.Kind.Valid || arg.Kind.String == row.Kind) && (!arg.TargetID.Valid || arg.TargetID.String == row.TargetID) {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (f *fakeJobs) RetryFailedSearchJob(_ context.Context, entryID string) (int64, error) {
	if _, ok := f.failed[KindSearch+"/"+entryID]; !ok {
		return 0, nil
	}
	delete(f.failed, KindSearch+"/"+entryID)
	return 1, nil
}

func (f *fakeJobs) Requeue(
	_ context.Context, blueprintID, phase string, retryFailed bool, ticket renderqueue.Ticket,
) (bool, error) {
	if ticket != renderqueue.Staff() {
		return false, errors.New("a staff re-run must use the staff ticket")
	}
	key := phase + "/" + blueprintID
	if _, failed := f.failed[key]; (failed && !retryFailed) || f.open[key] {
		return false, nil
	}
	delete(f.failed, key)
	f.open[key] = true
	f.requeued = append(f.requeued, key)
	return true, nil
}

func (f *fakeJobs) ListLiveEntryLeafIDs(_ context.Context, entryID string) ([]string, error) {
	return f.leaves[entryID], nil
}

func (f *fakeJobs) Add(_ context.Context, _ audit.Staff, record audit.Record) error {
	f.audits = append(f.audits, record)
	return nil
}

func service(f *fakeJobs) *Service {
	return New(f, f, f, func() { f.nudged++ })
}

var staffer = audit.Staff{Username: "owner"}

// A failed phase goes through the requeuer once, with its failure in the
// audit row; a second re-run finds nothing failed.
func TestRerunPhase(t *testing.T) {
	ctx := context.Background()
	f := newFake(db.ListFailedJobsRow{Kind: KindPreview, TargetID: "bp", EntryID: "e", FailureCode: "render_timeout"})
	s := service(f)
	if err := s.Rerun(ctx, staffer, KindPreview, "bp", "fixed"); err != nil {
		t.Fatal(err)
	}
	if len(f.requeued) != 1 || f.requeued[0] != "preview/bp" {
		t.Fatalf("requeued %v", f.requeued)
	}
	if len(f.audits) != 1 || f.audits[0].TargetID != "bp" || f.audits[0].Action != "job.rerun" ||
		f.audits[0].Before.(map[string]any)["failure_code"] != "render_timeout" {
		t.Fatalf("audits %+v", f.audits)
	}
	if err := s.Rerun(ctx, staffer, KindPreview, "bp", ""); !errors.Is(err, ErrNotFailed) {
		t.Fatalf("second re-run: %v", err)
	}
}

// A failed search job is reset and the search dispatcher woken.
func TestRerunSearch(t *testing.T) {
	f := newFake(db.ListFailedJobsRow{Kind: KindSearch, TargetID: "e", EntryID: "e"})
	if err := service(f).Rerun(context.Background(), staffer, KindSearch, "e", ""); err != nil {
		t.Fatal(err)
	}
	if f.nudged != 1 || len(f.requeued) != 0 || len(f.audits) != 1 {
		t.Fatalf("nudged %d requeued %v audits %d", f.nudged, f.requeued, len(f.audits))
	}
}

// Bad input is refused before anything is queued.
func TestRerunRefuses(t *testing.T) {
	ctx := context.Background()
	f := newFake(db.ListFailedJobsRow{Kind: KindAnalysis, TargetID: "bp"})
	s := service(f)
	if err := s.Rerun(ctx, staffer, "other", "bp", ""); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unknown kind: %v", err)
	}
	if err := s.Rerun(ctx, staffer, KindAnalysis, "bp", strings.Repeat("x", audit.MaxReasonRunes+1)); !errors.Is(err, audit.ErrInvalidRequest) {
		t.Fatalf("long reason: %v", err)
	}
	if err := s.Rerun(ctx, staffer, KindPreview, "bp", ""); !errors.Is(err, ErrNotFailed) {
		t.Fatalf("other phase: %v", err)
	}
	if len(f.requeued) != 0 || len(f.audits) != 0 {
		t.Fatalf("requeued %v audits %v", f.requeued, f.audits)
	}
	if _, err := s.ListFailed(ctx, "other"); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("list unknown kind: %v", err)
	}
}

// An entry re-run queues every current leaf's phase (done or failed), or one
// leaf; leaves already queued are skipped, and nothing queued is an error.
func TestRerunEntry(t *testing.T) {
	ctx := context.Background()
	f := newFake(db.ListFailedJobsRow{Kind: KindAnalysis, TargetID: "b2", EntryID: "e"})
	f.leaves["e"] = []string{"b1", "b2"}
	f.open[KindAnalysis+"/b1"] = true
	s := service(f)
	if err := s.RerunEntry(ctx, staffer, "e", KindAnalysis, "", "new table"); err != nil {
		t.Fatal(err)
	}
	if len(f.requeued) != 1 || f.requeued[0] != "analysis/b2" {
		t.Fatalf("requeued %v", f.requeued)
	}
	if len(f.audits) != 1 || f.audits[0].Action != "entry.rerun" || f.audits[0].After.(map[string]any)["phase"] != KindAnalysis {
		t.Fatalf("audits %+v", f.audits)
	}
	if err := s.RerunEntry(ctx, staffer, "e", KindAnalysis, "", ""); !errors.Is(err, ErrAlreadyQueued) {
		t.Fatalf("all queued: %v", err)
	}
	if err := s.RerunEntry(ctx, staffer, "e", KindPreview, "b1", ""); err != nil {
		t.Fatal(err)
	}
	if got := f.requeued[len(f.requeued)-1]; got != "preview/b1" {
		t.Fatalf("one leaf requeued %q", got)
	}
	if err := s.RerunEntry(ctx, staffer, "e", KindPreview, "elsewhere", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("leaf of another entry: %v", err)
	}
	if err := s.RerunEntry(ctx, staffer, "gone", KindPreview, "", ""); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("entry not live: %v", err)
	}
	if err := s.RerunEntry(ctx, staffer, "e", KindSearch, "", ""); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("search phase: %v", err)
	}
}
