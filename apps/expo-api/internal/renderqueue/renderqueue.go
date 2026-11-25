// Package renderqueue decides whose render work a preview or analysis intent
// is, where it sits in the queue, and whether an uploader may queue more.
//
// Every intent row carries a queue key (the uploader: an account, or the
// keyed hash of an anonymous address) and order_at, its place. The place is
// fixed when the work is queued (Place): the queued time plus the delay of
// its kind, and one uploader's leaves are spaced Gap apart, starting after
// that uploader's last waiting leaf. The preview dispatcher sends the lowest
// order_at while the render worker holds fewer than Window jobs. So a big
// book is interleaved with everyone else's work (its 50th leaf is placed as
// if queued 49 gaps later), it renders at full speed when nobody else waits,
// and nothing starves: places are times, and work queued later is placed
// later. The cap bounds how many leaves one key may have waiting; an upload
// that would pass it is refused whole, never rendered in part.
package renderqueue

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"factorio-expo/apps/expo-api/internal/fault"
)

// ErrFull is returned when queuing the work would put its uploader over the
// cap of waiting leaves.
var ErrFull = fault.New(fault.TooMany, "render_queue_full",
	"you have too many blueprints waiting to render; try again once some have finished")

// Keys of work that has no uploader.
const (
	// StaffKey is a staff re-run: no delay, no spacing, no cap.
	StaffKey = "staff"
	// SystemKey is work the API queues itself (a repaired lost intent).
	SystemKey = "system"
)

// Policy is the queue's tuning. Defaults are guesses until the capacity test
// measures real renders.
type Policy struct {
	// Window is how many runner jobs may be unfinished at once: one running
	// and one ready next keeps the worker busy while the API picks.
	Window int
	// CapAnonymous and CapSignedIn bound the waiting leaves per key.
	CapAnonymous int
	CapSignedIn  int
	// Delays added to the queued time, by kind of work. Quick previews and
	// staff re-runs have none.
	DelaySignedIn  time.Duration
	DelayAnonymous time.Duration
	// DelayRerender is added to the requester's own delay for a re-render of
	// an evicted full preview.
	DelayRerender time.Duration
	DelayRepair   time.Duration
	// LeafGap spaces one uploader's leaves: about one render.
	LeafGap time.Duration
}

// Defaults are the starting values (API_RENDER_* overrides them).
func Defaults() Policy {
	return Policy{
		Window:         2,
		CapAnonymous:   100,
		CapSignedIn:    300,
		DelaySignedIn:  30 * time.Second,
		DelayAnonymous: 2 * time.Minute,
		DelayRerender:  time.Minute,
		DelayRepair:    2 * time.Minute,
		LeafGap:        30 * time.Second,
	}
}

// Requester is who asks for work: a signed-in account, else the keyed hash
// of the address (nil when unknown).
type Requester struct {
	UserID string
	IPHash []byte
}

// Ticket is a place in the queue for one piece of work.
type Ticket struct {
	Key   string
	Delay time.Duration
	// Gap spaces the key's leaves (Place); 0 places them at the queued time
	// plus Delay, ahead of the key's other work.
	Gap time.Duration
	// Cap is the most waiting leaves Key may have; 0 means no cap.
	Cap int
}

// Upload is the ticket of an upload or a new revision.
func (p Policy) Upload(requester Requester) Ticket {
	if requester.UserID != "" {
		return Ticket{Key: requester.key(), Delay: p.DelaySignedIn, Gap: p.LeafGap, Cap: p.CapSignedIn}
	}
	return Ticket{Key: requester.key(), Delay: p.DelayAnonymous, Gap: p.LeafGap, Cap: p.CapAnonymous}
}

// QuickPreview is the ticket of a quick preview: someone is watching it, so
// it goes first, and it is not spaced behind its uploader's other work. It
// still counts against the cap.
func (p Policy) QuickPreview(requester Requester) Ticket {
	ticket := p.Upload(requester)
	ticket.Delay, ticket.Gap = 0, 0
	return ticket
}

// Rerender is the ticket of a re-render of an evicted full preview. It is
// the requester's (the visitor who opened it), not the entry author's.
func (p Policy) Rerender(requester Requester) Ticket {
	ticket := p.Upload(requester)
	ticket.Delay += p.DelayRerender
	return ticket
}

// Repair is the ticket of a repaired lost intent.
func (p Policy) Repair() Ticket { return Ticket{Key: SystemKey, Delay: p.DelayRepair} }

// Staff is the ticket of a staff re-run.
func Staff() Ticket { return Ticket{Key: StaffKey} }

func (r Requester) key() string {
	if r.UserID != "" {
		return "u:" + r.UserID
	}
	return "ip:" + hex.EncodeToString(r.IPHash)
}

// Counter counts a key's waiting leaves; *db.Queries satisfies it.
type Counter interface {
	CountWaitingLeaves(ctx context.Context, queueKey string) (int64, error)
}

// PlaceQueries is what Place needs; *db.Queries satisfies it.
type PlaceQueries interface {
	LockQueueKey(ctx context.Context, queueKey string) error
	LastOrderAt(ctx context.Context, queueKey string) (pgtype.Timestamptz, error)
}

// Place returns the order_at of n leaves queued at now under ticket: now
// plus Delay, spaced Gap apart after the key's last waiting leaf. In a
// transaction it locks the key until the end, so a concurrent request of the
// same key places (and counts, Check) after this one.
func Place(ctx context.Context, queries PlaceQueries, ticket Ticket, now time.Time, n int) ([]pgtype.Timestamptz, error) {
	start := now.Add(ticket.Delay)
	if ticket.Gap > 0 || ticket.Cap > 0 {
		if err := queries.LockQueueKey(ctx, ticket.Key); err != nil {
			return nil, fmt.Errorf("lock render queue key: %w", err)
		}
	}
	if ticket.Gap > 0 {
		last, err := queries.LastOrderAt(ctx, ticket.Key)
		if err != nil {
			return nil, fmt.Errorf("read last queue place: %w", err)
		}
		if last.Valid && last.Time.Add(ticket.Gap).After(start) {
			start = last.Time.Add(ticket.Gap)
		}
	}
	places := make([]pgtype.Timestamptz, n)
	for i := range places {
		places[i] = pgtype.Timestamptz{Time: start.Add(time.Duration(i) * ticket.Gap), Valid: true}
	}
	return places, nil
}

// Check returns ErrFull when the key's waiting leaves plus more pass its
// cap. After writing the intents in the transaction Place locked, more is 0
// (they count already). Without a transaction nothing is locked, so
// concurrent requests of one key may pass it together; callers bound those by
// their rate limit (one leaf per re-render request).
func Check(ctx context.Context, queries Counter, ticket Ticket, more int) error {
	if ticket.Cap <= 0 {
		return nil
	}
	waiting, err := queries.CountWaitingLeaves(ctx, ticket.Key)
	if err != nil {
		return fmt.Errorf("count waiting leaves: %w", err)
	}
	if waiting+int64(more) > int64(ticket.Cap) {
		return ErrFull
	}
	return nil
}

// ReusedLeaf is a waiting leaf phase that took the stored result of another
// leaf with the same exchange string (catalog reuse), so it is not rendered.
type ReusedLeaf struct {
	EntryID     string
	BlueprintID string
	// Phase is progress.PhasePreview or PhaseAnalysis.
	Phase string
	// FailureCode is set when a final failure was copied.
	FailureCode string
}
