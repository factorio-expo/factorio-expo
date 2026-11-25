package maintenance

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"factorio-expo/apps/expo-api/internal/audit"
)

type audits struct {
	records []audit.Record
	failAt  int // 1-based record that fails; 0: none
}

func (a *audits) Add(_ context.Context, _ audit.Staff, record audit.Record) error {
	if a.failAt == len(a.records)+1 {
		return errors.New("audit down")
	}
	a.records = append(a.records, record)
	return nil
}

func count(n int64) func(context.Context, *int64) (CleanupResult, error) {
	return func(context.Context, *int64) (CleanupResult, error) { return CleanupResult{Count: n}, nil }
}

func totals(context.Context) (CleanupPreviewTotals, error) {
	return CleanupPreviewTotals{FullFiles: 2}, nil
}

func TestScheduleRunsAtStartThenEveryIntervalAndJoins(t *testing.T) {
	var every, once, manual atomic.Int64
	registry := New([]Task{
		{Name: "every", Every: time.Millisecond, Run: func(context.Context, *int64) (CleanupResult, error) {
			every.Add(1)
			return CleanupResult{}, nil
		}},
		{Name: "once", AtStart: true, Run: func(context.Context, *int64) (CleanupResult, error) {
			once.Add(1)
			return CleanupResult{}, nil
		}},
		{Name: "manual", Run: func(context.Context, *int64) (CleanupResult, error) {
			manual.Add(1)
			return CleanupResult{}, nil
		}},
	}, StaffDeps{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		registry.Schedule(ctx)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for every.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Schedule did not return after cancel")
	}
	if every.Load() < 3 || once.Load() != 1 || manual.Load() != 0 {
		t.Fatalf("runs: every %d, once %d, manual %d", every.Load(), once.Load(), manual.Load())
	}
}

func TestOverviewListsStaffTasksInOrder(t *testing.T) {
	registry := New([]Task{
		{Name: "a", Run: count(0), Staff: &StaffView{Title: "A", Count: func(context.Context, *int64) (Pending, error) {
			return Pending{Count: 3}, nil
		}}},
		{Name: "scheduled-only", Every: time.Hour, Run: count(0)},
		{Name: "b", Run: count(0), Staff: &StaffView{Title: "B", TakesTarget: true, Count: func(_ context.Context, target *int64) (Pending, error) {
			bytes := *target * 2
			return Pending{Count: 1, Bytes: &bytes}, nil
		}}},
	}, StaffDeps{Totals: totals})

	overview, err := registry.Overview(context.Background(), nil)
	if err != nil || len(overview.Cleanups) != 2 || overview.Cleanups[0].Name != "a" || overview.Cleanups[0].Pending != 3 ||
		overview.Cleanups[1].Name != "b" || !overview.Cleanups[1].TakesTarget || overview.Cleanups[1].Pending != 0 ||
		overview.Previews.FullFiles != 2 || overview.TargetBytes != nil {
		t.Fatalf("overview without a target = %+v, %v", overview, err)
	}
	target := int64(10)
	overview, err = registry.Overview(context.Background(), &target)
	if err != nil || overview.Cleanups[1].Pending != 1 || *overview.Cleanups[1].PendingBytes != 20 {
		t.Fatalf("overview with a target = %+v, %v", overview, err)
	}
	negative := int64(-1)
	if _, err := registry.Overview(context.Background(), &negative); !errors.Is(err, ErrTargetRequired) {
		t.Fatalf("negative target: %v", err)
	}
}

func TestRunAuditsBeforeAndAfter(t *testing.T) {
	log := &audits{}
	ran := false
	registry := New([]Task{
		{Name: "a", Staff: &StaffView{Title: "A"}, Run: func(context.Context, *int64) (CleanupResult, error) {
			ran = true
			if len(log.records) != 1 || log.records[0].Action != "cleanup.run" {
				t.Errorf("the request was not audited before the run: %+v", log.records)
			}
			bytes := int64(5)
			return CleanupResult{Count: 2, Bytes: &bytes}, nil
		}},
		{Name: "needs-target", Run: count(0), Staff: &StaffView{Title: "T", TakesTarget: true}},
		{Name: "hidden", Run: count(0)},
	}, StaffDeps{Audit: log})

	result, err := registry.Run(context.Background(), audit.Staff{}, "a", nil, "tidy up")
	if err != nil || !ran || result.Name != "a" || result.Count != 2 || *result.Bytes != 5 {
		t.Fatalf("run = %+v, %v", result, err)
	}
	if len(log.records) != 2 || log.records[1].Action != "cleanup.result" ||
		log.records[1].After.(map[string]any)["count"] != int64(2) || log.records[1].TargetID != "a" {
		t.Fatalf("audit = %+v", log.records)
	}

	for name, target := range map[string]*int64{"needs-target": nil} {
		if _, err := registry.Run(context.Background(), audit.Staff{}, name, target, "x"); !errors.Is(err, ErrTargetRequired) {
			t.Errorf("%s without a target: %v", name, err)
		}
	}
	for _, name := range []string{"hidden", "nope"} {
		if _, err := registry.Run(context.Background(), audit.Staff{}, name, nil, "x"); !errors.Is(err, ErrUnknown) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRunDoesNotActWithoutItsAuditRow(t *testing.T) {
	log := &audits{failAt: 1}
	ran := false
	registry := New([]Task{
		{Name: "a", Staff: &StaffView{Title: "A"}, Run: func(context.Context, *int64) (CleanupResult, error) {
			ran = true
			return CleanupResult{}, nil
		}},
	}, StaffDeps{Audit: log})
	if _, err := registry.Run(context.Background(), audit.Staff{}, "a", nil, "x"); err == nil || ran {
		t.Fatalf("run with a failing audit: err %v, ran %v", err, ran)
	}
}

func TestRunRecordsAFailedRun(t *testing.T) {
	log := &audits{}
	boom := errors.New("boom")
	registry := New([]Task{
		{Name: "a", Staff: &StaffView{Title: "A"}, Run: func(context.Context, *int64) (CleanupResult, error) {
			return CleanupResult{Count: 1}, boom
		}},
	}, StaffDeps{Audit: log})
	result, err := registry.Run(context.Background(), audit.Staff{}, "a", nil, "x")
	if !errors.Is(err, boom) || result.Count != 1 || log.records[1].After.(map[string]any)["error"] != true {
		t.Fatalf("failed run = %+v, %v, audit %+v", result, err, log.records)
	}
}
