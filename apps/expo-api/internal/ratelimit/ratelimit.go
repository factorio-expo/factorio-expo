// Package ratelimit is an in-memory keyed token-bucket limiter. The API runs
// as a single process, so buckets live in memory; edge rate limiting
// rules are the outer layer in front of it.
package ratelimit

import (
	"math"
	"sync"
	"time"
)

// Rule is a token bucket: Burst requests at once, refilled at one token per
// Every.
type Rule struct {
	Name  string
	Burst float64
	Every time.Duration
}

type bucket struct {
	tokens  float64
	updated time.Time
	// full is when the bucket will have refilled completely; after that it
	// is indistinguishable from a fresh one and can be dropped.
	full time.Time
}

// Limiter holds every bucket. It is safe for concurrent use.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	maxKeys int
	now     func() time.Time
}

// New builds a limiter tracking at most maxKeys buckets. When the table is
// full and no idle bucket can be dropped, new keys are refused: under a key
// flood the limiter fails closed rather than growing without bound.
func New(maxKeys int) *Limiter {
	return &Limiter{buckets: map[string]*bucket{}, maxKeys: maxKeys, now: time.Now}
}

// Allow takes one token from the bucket for (rule, key). When the bucket is
// empty it reports how long until the next token.
func (l *Limiter) Allow(rule Rule, key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	id := rule.Name + "\x00" + key
	b, ok := l.buckets[id]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			l.sweepLocked(now)
			if len(l.buckets) >= l.maxKeys {
				return false, rule.Every
			}
		}
		b = &bucket{tokens: rule.Burst, updated: now}
		l.buckets[id] = b
	}

	b.tokens = b.refilled(rule, now)
	b.updated = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	b.full = now.Add(time.Duration((rule.Burst - b.tokens) * float64(rule.Every)))
	if allowed {
		return true, 0
	}
	return false, untilToken(rule, b.tokens)
}

// Peek reports what Allow would answer for (rule, key) now without taking a
// token, for a budget charged only when the request succeeds (Allow then).
func (l *Limiter) Peek(rule Rule, key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[rule.Name+"\x00"+key]
	if !ok {
		return true, 0
	}
	if tokens := b.refilled(rule, l.now()); tokens < 1 {
		return false, untilToken(rule, tokens)
	}
	return true, 0
}

// refilled is the bucket's token count at now.
func (b *bucket) refilled(rule Rule, now time.Time) float64 {
	return math.Min(rule.Burst, b.tokens+now.Sub(b.updated).Seconds()/rule.Every.Seconds())
}

// untilToken is how long a bucket holding tokens (< 1) waits for the next.
func untilToken(rule Rule, tokens float64) time.Duration {
	return time.Duration((1 - tokens) * float64(rule.Every))
}

// Sweep drops buckets idle long enough to have refilled completely; they are
// indistinguishable from fresh ones.
func (l *Limiter) Sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(l.now())
}

func (l *Limiter) sweepLocked(now time.Time) {
	for id, b := range l.buckets {
		if !now.Before(b.full) {
			delete(l.buckets, id)
		}
	}
}

// Run sweeps periodically until stop is closed.
func (l *Limiter) Run(interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.Sweep()
		case <-stop:
			return
		}
	}
}
