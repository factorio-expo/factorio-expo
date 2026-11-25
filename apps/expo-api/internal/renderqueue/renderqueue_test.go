package renderqueue

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestTickets(t *testing.T) {
	t.Parallel()
	policy := Defaults()
	address := []byte{0xab, 0xcd}
	anonymous := Requester{IPHash: address}
	user := Requester{UserID: "u1", IPHash: address}
	for name, test := range map[string]struct {
		got, want Ticket
	}{
		"anonymous upload": {policy.Upload(anonymous), Ticket{Key: "ip:" + hex.EncodeToString(address), Delay: 2 * time.Minute, Gap: 30 * time.Second, Cap: 100}},
		"signed-in upload": {policy.Upload(user), Ticket{Key: "u:u1", Delay: 30 * time.Second, Gap: 30 * time.Second, Cap: 300}},
		"quick preview":    {policy.QuickPreview(anonymous), Ticket{Key: "ip:abcd", Cap: 100}},
		"re-render":        {policy.Rerender(user), Ticket{Key: "u:u1", Delay: 90 * time.Second, Gap: 30 * time.Second, Cap: 300}},
		"staff":            {Staff(), Ticket{Key: StaffKey}},
		"repair":           {policy.Repair(), Ticket{Key: SystemKey, Delay: 2 * time.Minute}},
	} {
		if test.got != test.want {
			t.Errorf("%s = %+v, want %+v", name, test.got, test.want)
		}
	}
}

type fakeQueue struct {
	waiting int64
	last    pgtype.Timestamptz
	locked  []string
}

func (f *fakeQueue) CountWaitingLeaves(context.Context, string) (int64, error) { return f.waiting, nil }
func (f *fakeQueue) LastOrderAt(context.Context, string) (pgtype.Timestamptz, error) {
	return f.last, nil
}
func (f *fakeQueue) LockQueueKey(_ context.Context, key string) error {
	f.locked = append(f.locked, key)
	return nil
}

func TestCheck(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	queue := &fakeQueue{waiting: 2}
	ticket := Ticket{Key: "k", Cap: 3}
	if err := Check(ctx, queue, ticket, 1); err != nil {
		t.Fatalf("at the cap = %v", err)
	}
	if err := Check(ctx, queue, ticket, 2); !errors.Is(err, ErrFull) {
		t.Fatalf("over the cap = %v", err)
	}
	if err := Check(ctx, queue, Ticket{Key: StaffKey}, 1000); err != nil {
		t.Fatalf("no cap = %v", err)
	}
}

func TestPlace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	ticket := Ticket{Key: "k", Delay: time.Minute, Gap: 10 * time.Second, Cap: 5}
	queue := &fakeQueue{last: pgtype.Timestamptz{Time: now.Add(5 * time.Minute), Valid: true}}
	places, err := Place(ctx, queue, ticket, now, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i, place := range places {
		if want := now.Add(5*time.Minute + time.Duration(i+1)*10*time.Second); !place.Time.Equal(want) {
			t.Fatalf("place %d = %v, want %v", i, place.Time, want)
		}
	}
	if len(queue.locked) != 1 || queue.locked[0] != "k" {
		t.Fatalf("locked %v", queue.locked)
	}
	// Unspaced work (a quick preview) ignores the key's waiting leaves.
	places, _ = Place(ctx, queue, Ticket{Key: "k", Cap: 5}, now, 1)
	if !places[0].Time.Equal(now) {
		t.Fatalf("unspaced place = %v", places[0].Time)
	}
}
