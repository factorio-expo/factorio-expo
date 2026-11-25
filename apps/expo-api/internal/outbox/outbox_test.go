package outbox

import (
	"context"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type handlerFunc func(key string) []Result

func (f handlerFunc) Handle(_ context.Context, key string) []Result { return f(key) }

func testPolicy() Policy {
	return Policy{MaxRetries: 3, Base: 5 * time.Second, Max: 5 * time.Minute, Lease: time.Minute}
}

func newQueue(t *testing.T, outcome func() Outcome) (*Queue[string], *MemoryStore, *clock) {
	t.Helper()
	clk := &clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := NewMemoryStore(clk.Now)
	handler := handlerFunc(func(string) []Result { return []Result{{Outcome: outcome()}} })
	return New[string]("test", store, handler, testPolicy(), Options{PollInterval: 3 * time.Second, Now: clk.Now}), store, clk
}

func TestPolicyDelayDoublesAndCaps(t *testing.T) {
	t.Parallel()
	p := testPolicy()
	for retries, want := range map[int32]time.Duration{
		1: 5 * time.Second, 2: 10 * time.Second, 3: 20 * time.Second, 7: 5 * time.Minute, 30: 5 * time.Minute,
	} {
		if got := p.Delay(retries); got != want {
			t.Fatalf("Delay(%d) = %v, want %v", retries, got, want)
		}
	}
}

func TestSucceededRemovesRow(t *testing.T) {
	t.Parallel()
	queue, store, _ := newQueue(t, Succeeded)
	store.Enqueue("a")
	if err := queue.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Row("a"); ok {
		t.Fatal("row survived success")
	}
}

func TestTransientRetriesWithBackoffThenFailsAtBudget(t *testing.T) {
	t.Parallel()
	queue, store, clk := newQueue(t, func() Outcome { return Transient("dispatch_failed", "runner down") })
	store.Enqueue("a")
	wantDelay := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	for i, delay := range wantDelay {
		start := clk.Now()
		if err := queue.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		row, _ := store.Row("a")
		if row.State != "pending" || row.Retries != int32(i+1) || !row.AvailableAt.Equal(start.Add(delay)) ||
			row.Code != "dispatch_failed" || row.Detail != "runner down" {
			t.Fatalf("after failure %d: %+v", i+1, row)
		}
		clk.Advance(delay)
	}
	if err := queue.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	row, _ := store.Row("a")
	if row.State != "failed" || row.Retries != 3 || row.Code != "dispatch_failed" || row.Detail != "runner down" {
		t.Fatalf("budget exhausted: %+v", row)
	}
}

func TestPermanentFailsImmediatelyWithDetail(t *testing.T) {
	t.Parallel()
	queue, store, _ := newQueue(t, func() Outcome { return Permanent("bad_thing", "why") })
	store.Enqueue("a")
	if err := queue.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	row, _ := store.Row("a")
	if row.State != "failed" || row.Retries != 0 || row.Detail != "why" {
		t.Fatalf("permanent: %+v", row)
	}
}

func TestWaitingReschedulesWithoutSpendingBudget(t *testing.T) {
	t.Parallel()
	queue, store, clk := newQueue(t, Waiting)
	store.Enqueue("a")
	start := clk.Now()
	if err := queue.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	row, _ := store.Row("a")
	if row.State != "pending" || row.Retries != 0 || !row.AvailableAt.Equal(start.Add(3*time.Second)) {
		t.Fatalf("waiting row: %+v", row)
	}
	queue2, store2, clk2 := newQueue(t, func() Outcome { return WaitFor(time.Minute) })
	store2.Enqueue("a")
	start = clk2.Now()
	_ = queue2.PollOnce(context.Background())
	if row, _ := store2.Row("a"); !row.AvailableAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("WaitFor row: %+v", row)
	}
}

func TestClaimLeasesRowsAndExpiredLeaseIsReclaimed(t *testing.T) {
	t.Parallel()
	clk := &clock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	store := NewMemoryStore(clk.Now)
	store.Enqueue("a")
	first, _ := store.Claim(context.Background(), 10, time.Minute)
	second, _ := store.Claim(context.Background(), 10, time.Minute)
	if len(first) != 1 || len(second) != 0 {
		t.Fatalf("claims = %v then %v", first, second)
	}
	clk.Advance(time.Minute)
	if third, _ := store.Claim(context.Background(), 10, time.Minute); len(third) != 1 {
		t.Fatalf("expired lease not reclaimed: %v", third)
	}
}

func TestEnqueueRevivesFailedRowWithFreshBudget(t *testing.T) {
	t.Parallel()
	queue, store, _ := newQueue(t, func() Outcome { return Permanent("x", "y") })
	store.Enqueue("a")
	_ = queue.PollOnce(context.Background())
	if row, _ := store.Row("a"); row.State != "failed" {
		t.Fatalf("not failed: %+v", row)
	}
	store.Enqueue("a")
	if row, _ := store.Row("a"); row.State != "pending" || row.Retries != 0 || row.Code != "" {
		t.Fatalf("not revived: %+v", row)
	}
}

func TestNudgeWakesRunWithoutWaitingForTheTicker(t *testing.T) {
	t.Parallel()
	polled := make(chan struct{}, 4)
	store := NewMemoryStore(nil)
	handler := handlerFunc(func(string) []Result {
		polled <- struct{}{}
		return []Result{{Outcome: Succeeded()}}
	})
	queue := New[string]("test", store, handler, testPolicy(), Options{PollInterval: time.Hour})
	ctx := t.Context()
	go queue.Run(ctx)
	store.Enqueue("a")
	queue.Nudge()
	queue.Nudge() // coalesces; must not block
	select {
	case <-polled:
	case <-time.After(5 * time.Second):
		t.Fatal("Nudge did not wake the loop")
	}
}
