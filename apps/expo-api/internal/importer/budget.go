package importer

import (
	"sync"
	"time"
)

// Defaults of the per-site fetch budgets (Config.HourlyBudget, SiteBudgets).
const (
	// DefaultHourlyBudget is how many fetches an hour the API makes from one
	// site, summed over every caller.
	DefaultHourlyBudget = 300
	// DefaultGistHourlyBudget is GitHub's: its unauthenticated API allows 60
	// requests an hour per address, and the rest of the headroom is left to
	// the raw-file adapter's neighbours on the same address.
	DefaultGistHourlyBudget = 40
)

// budget is a token bucket of fetches: perHour tokens, refilled evenly over the
// hour. It is what keeps the site's own callers from spending a third party's
// quota for everyone, whatever the per-caller limits allow.
type budget struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	last   time.Time
}

func newBudget(perHour int, now time.Time) *budget {
	return &budget{tokens: float64(perHour), max: float64(perHour), last: now}
}

// take spends one fetch, or reports false.
func (b *budget) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = min(b.max, b.tokens+elapsed.Hours()*b.max)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
