// Package outbox owns the lifecycle of the durable dispatch intents
// (preview_jobs, analysis_jobs, entry_search_jobs): claim due rows under a
// lease, run a handler, and turn its outcome into done, retry, fail or
// reschedule. Retry budget and backoff live here once; handlers only do domain
// work and report an Outcome (transient or permanent is theirs to say).
package outbox

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Kind is what a handler decided about one slot of a claimed item.
type Kind int

const (
	// KindSucceeded finishes the slot: its row is removed.
	KindSucceeded Kind = iota
	// KindTransient retries the slot with backoff while budget remains, then fails it.
	KindTransient
	// KindPermanent fails the slot immediately.
	KindPermanent
	// KindWaiting keeps the slot as is and looks at it again after a delay,
	// without spending retry budget (work in flight, or a poll hiccup).
	KindWaiting
)

// Outcome is a handler's verdict for one slot.
type Outcome struct {
	Kind   Kind
	Code   string
	Detail string
	// After is the delay of a KindWaiting outcome; zero means the queue's
	// poll interval.
	After time.Duration
}

func Succeeded() Outcome { return Outcome{Kind: KindSucceeded} }

func Transient(code, detail string) Outcome {
	return Outcome{Kind: KindTransient, Code: code, Detail: detail}
}

func Permanent(code, detail string) Outcome {
	return Outcome{Kind: KindPermanent, Code: code, Detail: detail}
}

func Waiting() Outcome { return Outcome{Kind: KindWaiting} }

func WaitFor(after time.Duration) Outcome { return Outcome{Kind: KindWaiting, After: after} }

// Result addresses an Outcome to one slot of an item. Preview and analysis
// share a runner job but retry and fail independently, so one claimed item
// carries two slots; search uses the empty slot.
type Result struct {
	Slot    string
	Outcome Outcome
}

// ResolutionKind is what a Store must persist for a slot.
type ResolutionKind int

const (
	// ResolveDone removes the slot's row.
	ResolveDone ResolutionKind = iota
	// ResolveRetry counts a retry and makes the row due again at At.
	ResolveRetry
	// ResolveFail marks the row permanently failed.
	ResolveFail
	// ResolveRelease makes the row due again at At without counting a retry.
	ResolveRelease
)

// Resolution is the policy-decided persistence step for one slot.
type Resolution struct {
	Kind   ResolutionKind
	Code   string
	Detail string
	At     time.Time
}

// Store adapts one outbox table.
type Store[T any] interface {
	// Claim returns up to limit due items and leases them: their rows are not
	// due again for lease, so concurrent claimers (other replicas, a crashed
	// poll) never receive the same row twice.
	Claim(ctx context.Context, limit int32, lease time.Duration) ([]T, error)
	// Retries is how many retries the slot has already used.
	Retries(item T, slot string) int32
	// Apply persists the resolution of one slot.
	Apply(ctx context.Context, item T, slot string, resolution Resolution) error
}

// Handler runs the domain work for one claimed item.
type Handler[T any] interface {
	Handle(ctx context.Context, item T) []Result
}

// Policy is the one retry policy.
type Policy struct {
	// MaxRetries is how many retries a slot may use before a transient
	// outcome fails it.
	MaxRetries int32
	// Base and Max bound the exponential backoff: Base, 2*Base, ... up to Max.
	Base, Max time.Duration
	// Lease is how long a claimed row stays invisible to other claimers; it
	// must exceed the slowest handler.
	Lease time.Duration
}

// Delay is the backoff before retry number retries (1-based).
func (p Policy) Delay(retries int32) time.Duration {
	delay := p.Base
	for i := int32(1); i < retries && delay < p.Max; i++ {
		delay *= 2
	}
	if delay > p.Max {
		return p.Max
	}
	return delay
}

// Options configures a Queue.
type Options struct {
	PollInterval time.Duration
	BatchSize    int32
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Queue polls one Store and resolves handler outcomes with its Policy.
type Queue[T any] struct {
	name    string
	store   Store[T]
	handler Handler[T]
	policy  Policy
	options Options
	nudge   chan struct{}
}

// New builds a Queue; name only labels log lines.
func New[T any](name string, store Store[T], handler Handler[T], policy Policy, options Options) *Queue[T] {
	if options.PollInterval <= 0 {
		options.PollInterval = 3 * time.Second
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 50
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Queue[T]{
		name: name, store: store, handler: handler, policy: policy, options: options,
		nudge: make(chan struct{}, 1),
	}
}

// Nudge asks the running loop to poll now. It never blocks and coalesces.
func (q *Queue[T]) Nudge() {
	select {
	case q.nudge <- struct{}{}:
	default:
	}
}

// Run polls on the interval and on every Nudge until ctx is canceled.
func (q *Queue[T]) Run(ctx context.Context) {
	ticker := time.NewTicker(q.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-q.nudge:
		}
		if err := q.PollOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("poll outbox", "queue", q.name, "error", err)
		}
	}
}

// PollOnce claims due items and resolves every slot a handler reports.
func (q *Queue[T]) PollOnce(ctx context.Context) error {
	items, err := q.store.Claim(ctx, q.options.BatchSize, q.policy.Lease)
	if err != nil {
		return err
	}
	for _, item := range items {
		for _, result := range q.handler.Handle(ctx, item) {
			resolution := q.resolve(item, result)
			if err := q.store.Apply(ctx, item, result.Slot, resolution); err != nil {
				slog.Error("apply outbox outcome", "queue", q.name, "slot", result.Slot, "error", err)
			}
		}
	}
	return nil
}

func (q *Queue[T]) resolve(item T, result Result) Resolution {
	outcome := result.Outcome
	now := q.options.Now()
	switch outcome.Kind {
	case KindSucceeded:
		return Resolution{Kind: ResolveDone}
	case KindWaiting:
		after := outcome.After
		if after <= 0 {
			after = q.options.PollInterval
		}
		return Resolution{Kind: ResolveRelease, At: now.Add(after)}
	}
	retries := q.store.Retries(item, result.Slot)
	if outcome.Kind == KindPermanent || retries >= q.policy.MaxRetries {
		return Resolution{Kind: ResolveFail, Code: outcome.Code, Detail: outcome.Detail}
	}
	return Resolution{
		Kind: ResolveRetry, Code: outcome.Code, Detail: outcome.Detail,
		At: now.Add(q.policy.Delay(retries + 1)),
	}
}
