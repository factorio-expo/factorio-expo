package views

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	db "factorio-expo/apps/expo-api/internal/db/generated"
)

const (
	entryA  = "aaaaaaaaaaaa"
	entryB  = "bbbbbbbbbbbb"
	firefox = "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0"
)

var (
	alice = netip.MustParseAddr("203.0.113.1")
	bob   = netip.MustParseAddr("203.0.113.2")
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestCounterCountsOneViewPerVisitorPerEntryPerDay(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)}
	counter := NewCounter(Options{Now: c.now})

	for _, step := range []struct {
		entry string
		addr  netip.Addr
		ua    string
		want  Result
	}{
		{entryA, alice, firefox, Counted},
		{entryA, alice, firefox, Repeat},
		{entryB, alice, firefox, Counted},
		{entryA, bob, firefox, Counted},
		// Another browser on the same address is another visitor.
		{entryA, alice, firefox + " other", Counted},
	} {
		if got := counter.Record(step.entry, step.addr, step.ua); got != step.want {
			t.Fatalf("Record(%s, %s) = %v, want %v", step.entry, step.addr, got, step.want)
		}
	}

	// A new UTC day forgets yesterday's visitors.
	c.t = c.t.Add(2 * time.Hour)
	if got := counter.Record(entryA, alice, firefox); got != Counted {
		t.Fatalf("next day Record = %v, want Counted", got)
	}

	got := counter.Take()
	if got[entryA] != 4 || got[entryB] != 1 || len(got) != 2 {
		t.Fatalf("Take = %v", got)
	}
	if again := counter.Take(); len(again) != 0 {
		t.Fatalf("second Take = %v, want empty", again)
	}
}

func TestCounterStopsAtItsBounds(t *testing.T) {
	counter := NewCounter(Options{MaxSeen: 2, MaxPending: 1})
	if got := counter.Record(entryA, alice, firefox); got != Counted {
		t.Fatalf("first = %v", got)
	}
	// A second entry does not fit the pending table.
	if got := counter.Record(entryB, alice, firefox); got != Full {
		t.Fatalf("second entry = %v, want Full", got)
	}
	if got := counter.Record(entryA, bob, firefox); got != Counted {
		t.Fatalf("second visitor = %v", got)
	}
	// The visitor table is full now.
	if got := counter.Record(entryA, netip.MustParseAddr("203.0.113.3"), firefox); got != Full {
		t.Fatalf("third visitor = %v, want Full", got)
	}
	if !counter.takeFull() || counter.takeFull() {
		t.Fatal("the full table should be reported exactly once")
	}
}

type fakeStore struct {
	added []db.AddEntryViewsParams
	err   error
}

func (s *fakeStore) AddEntryViews(_ context.Context, arg db.AddEntryViewsParams) error {
	if s.err != nil {
		return s.err
	}
	s.added = append(s.added, arg)
	return nil
}

func TestFlushWritesPendingViewsAndKeepsThemOnFailure(t *testing.T) {
	counter := NewCounter(Options{})
	store := &fakeStore{err: errors.New("database down")}
	flusher := NewFlusher(counter, store, time.Minute)

	counter.Record(entryA, alice, firefox)
	counter.Record(entryA, bob, firefox)
	flusher.Flush(context.Background())
	if len(store.added) != 0 {
		t.Fatal("a failed flush wrote views")
	}

	store.err = nil
	flusher.Flush(context.Background())
	if len(store.added) != 1 {
		t.Fatalf("flushes = %d, want 1", len(store.added))
	}
	added := store.added[0]
	if len(added.EntryIds) != 1 || added.EntryIds[0] != entryA || added.Views[0] != 2 {
		t.Fatalf("added = %+v, want the two views restored after the failure", added)
	}

	// Nothing pending: no statement.
	flusher.Flush(context.Background())
	if len(store.added) != 1 {
		t.Fatal("an empty flush wrote to the store")
	}
}

func TestIsBot(t *testing.T) {
	for ua, want := range map[string]bool{
		firefox: false,
		"":      true,
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":                                      true,
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/140.0.0.0 Safari/537.36": true,
		"Discordbot/2.0": true,
		"curl/8.5.0":     true,
	} {
		if got := IsBot(ua); got != want {
			t.Errorf("IsBot(%q) = %v, want %v", ua, got, want)
		}
	}
}
