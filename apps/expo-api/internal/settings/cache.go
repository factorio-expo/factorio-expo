package settings

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// DefaultCacheTTL is how long a read of the settings table is reused: a change
// made in the console reaches every request within this time.
const DefaultCacheTTL = 5 * time.Second

// Loader reads the stored rows (key to text value).
type Loader func(ctx context.Context) (map[string]string, error)

// Cache serves the effective settings from a short-lived copy of the table.
// It never fails: when the table cannot be read it keeps the last values
// (the defaults before the first read), so a database hiccup neither pauses
// nor reopens anything by itself.
type Cache struct {
	load Loader
	ttl  time.Duration
	now  func() time.Time

	mu     sync.Mutex
	values Values
	loaded time.Time
}

// NewCache builds a cache over load; a ttl of 0 means DefaultCacheTTL.
func NewCache(load Loader, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &Cache{load: load, ttl: ttl, now: time.Now, values: Defaults()}
}

// Get returns the effective settings.
func (c *Cache) Get(ctx context.Context) Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded.IsZero() && c.now().Sub(c.loaded) < c.ttl {
		return c.values
	}
	stored, err := c.load(ctx)
	if err != nil {
		slog.Error("read global settings", "error", err)
		// Retry on the next ttl, not on every request.
		c.loaded = c.now()
		return c.values
	}
	values, skipped := Merge(stored)
	for _, key := range skipped {
		slog.Warn("ignoring global setting", "key", key)
	}
	c.values, c.loaded = values, c.now()
	return c.values
}

// Invalidate drops the copy, so the next Get reads the table (after a write).
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.loaded = time.Time{}
	c.mu.Unlock()
}
