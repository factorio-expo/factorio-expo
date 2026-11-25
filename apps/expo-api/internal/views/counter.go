// Package views counts entry page views: one per visitor per entry per day,
// held in memory and added to entry_view_counts by a periodic flush.
//
// Visitors are told apart by a hash of their client address, user agent and
// the entry, seeded with a key that changes every UTC day. Neither the hash
// nor its inputs are ever stored, and yesterday's hashes are dropped with
// their key, so no personal data is kept.
package views

import (
	"hash/maphash"
	"net/netip"
	"sync"
	"time"
)

const (
	// DefaultMaxSeen bounds the visitors remembered per day (about 16 bytes
	// each). Once full, new visitors are not counted until the next day.
	DefaultMaxSeen = 1_000_000
	// DefaultMaxPending bounds the entries waiting for the next flush.
	DefaultMaxPending = 100_000
)

// Counter remembers today's visitors and the views not yet flushed. It is
// safe for concurrent use.
type Counter struct {
	mu         sync.Mutex
	now        func() time.Time
	day        int64
	seed       maphash.Seed
	seen       map[uint64]struct{}
	pending    map[string]int64
	maxSeen    int
	maxPending int
	// full is set once today's visitor table filled up; fullReported once
	// the flush loop logged it, so it is logged once a day.
	full, fullReported bool
}

// Options bounds a Counter; zero values take the defaults.
type Options struct {
	MaxSeen    int
	MaxPending int
	// Now is the clock (tests); default time.Now.
	Now func() time.Time
}

// NewCounter builds an empty Counter.
func NewCounter(options Options) *Counter {
	if options.MaxSeen <= 0 {
		options.MaxSeen = DefaultMaxSeen
	}
	if options.MaxPending <= 0 {
		options.MaxPending = DefaultMaxPending
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	c := &Counter{
		now:        options.Now,
		maxSeen:    options.MaxSeen,
		maxPending: options.MaxPending,
		pending:    map[string]int64{},
	}
	c.rotate(utcDay(c.now()))
	return c
}

// Result says what Record did with a view.
type Result int

const (
	// Counted: a new visitor today; the view waits for the next flush.
	Counted Result = iota
	// Repeat: this visitor was already counted for the entry today.
	Repeat
	// Full: a table is at its bound, so the view was dropped.
	Full
)

// Record counts one view of entryID unless the same visitor (address and
// user agent) already viewed it today.
func (c *Counter) Record(entryID string, addr netip.Addr, userAgent string) Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	if day := utcDay(c.now()); day != c.day {
		c.rotate(day)
	}

	var h maphash.Hash
	h.SetSeed(c.seed)
	ip := addr.As16()
	h.Write(ip[:])
	h.WriteString(userAgent)
	h.WriteByte(0)
	h.WriteString(entryID)
	key := h.Sum64()

	if _, ok := c.seen[key]; ok {
		return Repeat
	}
	if len(c.seen) >= c.maxSeen {
		c.full = true
		return Full
	}
	if _, ok := c.pending[entryID]; !ok && len(c.pending) >= c.maxPending {
		return Full
	}
	c.seen[key] = struct{}{}
	c.pending[entryID]++
	return Counted
}

// Take hands over the views counted since the last Take.
func (c *Counter) Take() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	taken := c.pending
	c.pending = map[string]int64{}
	return taken
}

// Restore puts back views a failed flush could not write, as far as the
// pending bound allows.
func (c *Counter) Restore(views map[string]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, n := range views {
		if _, ok := c.pending[id]; !ok && len(c.pending) >= c.maxPending {
			continue
		}
		c.pending[id] += n
	}
}

// takeFull reports (once per day) that the visitor table filled up.
func (c *Counter) takeFull() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.full || c.fullReported {
		return false
	}
	c.fullReported = true
	return true
}

// rotate starts a new day: a fresh seed and an empty visitor table.
func (c *Counter) rotate(day int64) {
	c.day = day
	c.seed = maphash.MakeSeed()
	c.seen = map[uint64]struct{}{}
	c.full, c.fullReported = false, false
}

func utcDay(t time.Time) int64 {
	return t.UTC().Unix() / 86400
}
