package preview

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/renderqueue"
)

// queueLeaf seeds a live leaf with exchange string str and queues both
// phases under ticket at now, placed by renderqueue.Place like an upload.
func queueLeaf(t *testing.T, pool *pgxpool.Pool, queries *db.Queries, ticket renderqueue.Ticket, now time.Time, str string) string {
	t.Helper()
	ctx := context.Background()
	_, blueprintID := seedBlueprint(t, pool, true)
	if _, err := pool.Exec(ctx,
		"UPDATE blueprints SET exchange_string = $2, exchange_sha256 = sha256(convert_to($2::text, 'UTF8')) WHERE id = $1",
		blueprintID, str); err != nil {
		t.Fatal(err)
	}
	places, err := renderqueue.Place(ctx, queries, ticket, now, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{slotPreview, slotAnalysis} {
		if err := queries.EnqueuePhaseJob(ctx, db.EnqueuePhaseJobParams{
			BlueprintID: blueprintID, Phase: phase, QueueKey: ticket.Key, OrderAt: places[0],
		}); err != nil {
			t.Fatal(err)
		}
	}
	return blueprintID
}

// dispatchable claims under window and sends every newly claimed leaf to a
// pretend runner job "job-<blueprint>", returning them in claim order.
func dispatchable(t *testing.T, queries *db.Queries, window int64) []string {
	t.Helper()
	ctx := context.Background()
	var sent []string
	for _, row := range claimWindow(t, queries, time.Nanosecond, window) {
		if row.PreviewRunnerJobID.Valid || row.AnalysisRunnerJobID.Valid {
			continue
		}
		job := pgtype.Text{String: "job-" + row.BlueprintID, Valid: true}
		for _, phase := range []string{slotPreview, slotAnalysis} {
			if err := queries.AssignRunnerPhaseJob(ctx, db.AssignRunnerPhaseJobParams{BlueprintID: row.BlueprintID, Phase: phase, RunnerJobID: job}); err != nil {
				t.Fatal(err)
			}
		}
		sent = append(sent, row.BlueprintID)
	}
	return sent
}

// sameLeaves compares claimed leaves as sets: claim rows come in no order.
func sameLeaves(got []string, want ...string) bool {
	return len(got) == len(want) && !slices.ContainsFunc(want, func(id string) bool { return !slices.Contains(got, id) })
}

// finish settles a leaf's runner job and imports its results.
func finish(t *testing.T, queries *db.Queries, blueprintID string) {
	t.Helper()
	ctx := context.Background()
	if err := queries.SettleRunnerJob(ctx, pgtype.Text{String: "job-" + blueprintID, Valid: true}); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{slotPreview, slotAnalysis} {
		if err := queries.DeletePhaseJob(ctx, db.DeletePhaseJobParams{BlueprintID: blueprintID, Phase: phase}); err != nil {
			t.Fatal(err)
		}
	}
}

// One uploader's book is spaced a gap per leaf, so work queued after it is
// interleaved instead of waiting for the whole book; quick previews are not
// spaced; and with the worker free a book runs at full speed.
func TestDispatchOrderInterleavesUploaders(t *testing.T) {
	pool, queries := sqlTestPool(t)
	policy := renderqueue.Defaults()
	t0 := time.Now().Add(-time.Hour)
	alice := policy.Upload(renderqueue.Requester{UserID: "alice"})
	bob := policy.Upload(renderqueue.Requester{UserID: "bob"})
	book := []string{
		queueLeaf(t, pool, queries, alice, t0, "a1"),
		queueLeaf(t, pool, queries, alice, t0, "a2"),
		queueLeaf(t, pool, queries, alice, t0, "a3"),
		queueLeaf(t, pool, queries, alice, t0, "a4"),
	}
	// Alice's leaves are placed at +30 s (her delay), +60 s, +90 s, +120 s.
	// Bob uploads at +70 s and is placed at +100 s: between her third and
	// fourth leaf, not after her whole book.
	single := queueLeaf(t, pool, queries, bob, t0.Add(70*time.Second), "b1")
	// Alice's quick preview at +40 s is not spaced behind her own book.
	quick := queueLeaf(t, pool, queries, policy.QuickPreview(renderqueue.Requester{UserID: "alice"}), t0.Add(40*time.Second), "q1")

	want := []string{book[0], quick, book[1], book[2], single, book[3]}
	var got []string
	for range want {
		sent := dispatchable(t, queries, 1)
		if len(sent) != 1 {
			t.Fatalf("window 1 sent %v after %v", sent, got)
		}
		if again := dispatchable(t, queries, 1); len(again) != 0 {
			t.Fatalf("a full window sent %v", again)
		}
		got = append(got, sent[0])
		finish(t, queries, sent[0])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// The window counts runner jobs that have not finished: a finished job whose
// results are still being imported frees its slot; so does a deleted entry's.
func TestDispatchWindow(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	ticket := renderqueue.Defaults().Upload(renderqueue.Requester{UserID: "carol"})
	now := time.Now().Add(-time.Hour)
	first := queueLeaf(t, pool, queries, ticket, now, "w1")
	second := queueLeaf(t, pool, queries, ticket, now, "w2")
	third := queueLeaf(t, pool, queries, ticket, now, "w3")

	if sent := dispatchable(t, queries, 2); !sameLeaves(sent, first, second) {
		t.Fatalf("window 2 sent %v", sent)
	}
	if sent := dispatchable(t, queries, 2); len(sent) != 0 {
		t.Fatalf("full window sent %v", sent)
	}
	if err := queries.SettleRunnerJob(ctx, pgtype.Text{String: "job-" + first, Valid: true}); err != nil {
		t.Fatal(err)
	}
	if sent := dispatchable(t, queries, 2); !sameLeaves(sent, third) {
		t.Fatalf("after one finished, sent %v", sent)
	}
	fourth := queueLeaf(t, pool, queries, ticket, now, "w4")
	if _, err := pool.Exec(ctx, `UPDATE entries SET deleted_at = now()
		WHERE id = (SELECT r.entry_id FROM blueprints b JOIN revisions r ON r.id = b.revision_id WHERE b.id = $1)`,
		second); err != nil {
		t.Fatal(err)
	}
	if sent := dispatchable(t, queries, 2); !sameLeaves(sent, fourth) {
		t.Fatalf("after a deletion, sent %v", sent)
	}
}

// Identical strings render once: a leaf whose string is in the runner waits.
func TestDispatchSendsAStringOnce(t *testing.T) {
	pool, queries := sqlTestPool(t)
	now := time.Now().Add(-time.Hour)
	policy := renderqueue.Defaults()
	first := queueLeaf(t, pool, queries, policy.Upload(renderqueue.Requester{UserID: "dave"}), now, "same")
	twin := queueLeaf(t, pool, queries, policy.Upload(renderqueue.Requester{UserID: "erin"}), now, "same")
	other := queueLeaf(t, pool, queries, policy.Upload(renderqueue.Requester{UserID: "erin"}), now, "other")

	if sent := dispatchable(t, queries, 10); !sameLeaves(sent, first, other) && !sameLeaves(sent, twin, other) {
		t.Fatalf("sent %v, want one of the identical leaves", sent)
	}
	if sent := dispatchable(t, queries, 10); len(sent) != 0 {
		t.Fatalf("duplicate sent while its twin renders: %v", sent)
	}
}

// Place spaces a key's leaves after its last waiting one and never before
// the ticket's delay.
func TestPlaceSpacesAfterTheLastWaitingLeaf(t *testing.T) {
	pool, queries := sqlTestPool(t)
	ctx := context.Background()
	ticket := renderqueue.Ticket{Key: "u:frank", Delay: 10 * time.Second, Gap: time.Minute}
	now := time.Now().Truncate(time.Second)
	queueLeaf(t, pool, queries, ticket, now, "f1")
	queueLeaf(t, pool, queries, ticket, now, "f2")
	places, err := renderqueue.Place(ctx, queries, ticket, now, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(10*time.Second + 2*time.Minute); !places[0].Time.Equal(want) ||
		!places[1].Time.Equal(want.Add(time.Minute)) {
		t.Fatalf("places = %v, %v; want %v and a gap later", places[0].Time, places[1].Time, want)
	}
	later := now.Add(time.Hour)
	if places, _ := renderqueue.Place(ctx, queries, ticket, later, 1); !places[0].Time.Equal(later.Add(10 * time.Second)) {
		t.Fatalf("a place long after the last = %v", places[0].Time)
	}
}
