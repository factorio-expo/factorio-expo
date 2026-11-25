package ratelimit

import (
	"testing"
	"time"
)

func TestBucketBurstRefillAndSweep(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(10)
	l.now = func() time.Time { return now }
	rule := Rule{Name: "r", Burst: 2, Every: time.Minute}

	for i := range 2 {
		if ok, _ := l.Allow(rule, "a"); !ok {
			t.Fatalf("request %d within burst was refused", i)
		}
	}
	ok, wait := l.Allow(rule, "a")
	if ok || wait != time.Minute {
		t.Fatalf("over burst: ok=%v wait=%v, want refused with 1m wait", ok, wait)
	}
	if ok, _ := l.Allow(rule, "b"); !ok {
		t.Fatal("another key must have its own bucket")
	}

	now = now.Add(time.Minute)
	if ok, _ := l.Allow(rule, "a"); !ok {
		t.Fatal("one token should have refilled after a minute")
	}

	now = now.Add(time.Hour)
	l.Sweep()
	if len(l.buckets) != 0 {
		t.Fatalf("buckets after sweep = %d, want 0", len(l.buckets))
	}
}

func TestPeekTakesNoToken(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(10)
	l.now = func() time.Time { return now }
	rule := Rule{Name: "r", Burst: 1, Every: time.Minute}

	for range 3 {
		if ok, _ := l.Peek(rule, "a"); !ok {
			t.Fatal("peeking a fresh key must admit it")
		}
	}
	if len(l.buckets) != 0 {
		t.Fatal("peeking must not create buckets")
	}
	if ok, _ := l.Allow(rule, "a"); !ok {
		t.Fatal("peeks must leave the token in place")
	}
	ok, wait := l.Peek(rule, "a")
	if ok || wait != time.Minute {
		t.Fatalf("peek of an empty bucket: ok=%v wait=%v, want refused with 1m wait", ok, wait)
	}
	now = now.Add(30 * time.Second)
	if ok, wait = l.Peek(rule, "a"); ok || wait != 30*time.Second {
		t.Fatalf("half refilled: ok=%v wait=%v, want refused with 30s wait", ok, wait)
	}
	now = now.Add(30 * time.Second)
	if ok, _ = l.Peek(rule, "a"); !ok {
		t.Fatal("a refilled bucket must be admitted")
	}
}

func TestFullTableFailsClosed(t *testing.T) {
	now := time.Unix(0, 0)
	l := New(1)
	l.now = func() time.Time { return now }
	rule := Rule{Name: "r", Burst: 5, Every: time.Minute}
	if ok, _ := l.Allow(rule, "a"); !ok {
		t.Fatal("first key should be admitted")
	}
	if ok, _ := l.Allow(rule, "b"); ok {
		t.Fatal("a new key must be refused while the table is full of active buckets")
	}
	now = now.Add(time.Hour)
	if ok, _ := l.Allow(rule, "b"); !ok {
		t.Fatal("an idle bucket should be swept to make room")
	}
}
