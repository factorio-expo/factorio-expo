package importer

import (
	"sync"
	"time"
)

// Defaults of the resolve cache.
const (
	DefaultCacheTTL        = 5 * time.Minute
	DefaultCacheMaxEntries = 128
	DefaultCacheMaxBytes   = 32 << 20
)

// cache keeps successful resolves for a few minutes, keyed by the canonical
// source link, so the same link pasted again costs no fetch. It is bounded in
// entries and in bytes (exchange strings reach megabytes); when full the oldest
// entry goes first.
type cache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	maxBytes   int64
	bytes      int64
	items      map[string]cacheItem
}

type cacheItem struct {
	result  Result
	expires time.Time
	stored  time.Time
}

func newCache(ttl time.Duration, maxEntries int, maxBytes int64) *cache {
	return &cache{ttl: ttl, maxEntries: maxEntries, maxBytes: maxBytes, items: map[string]cacheItem{}}
}

func (c *cache) get(key string, now time.Time) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[key]
	if !ok {
		return Result{}, false
	}
	if !now.Before(item.expires) {
		c.drop(key)
		return Result{}, false
	}
	return item.result, true
}

func (c *cache) put(key string, result Result, now time.Time) {
	size := int64(len(result.Exchange))
	if size > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drop(key)
	for k, item := range c.items {
		if !now.Before(item.expires) {
			c.drop(k)
		}
	}
	for len(c.items) >= c.maxEntries || c.bytes+size > c.maxBytes {
		oldest, found := "", false
		for k, item := range c.items {
			if !found || item.stored.Before(c.items[oldest].stored) {
				oldest, found = k, true
			}
		}
		if !found {
			break
		}
		c.drop(oldest)
	}
	c.items[key] = cacheItem{result: result, expires: now.Add(c.ttl), stored: now}
	c.bytes += size
}

// drop removes key; the caller holds the lock.
func (c *cache) drop(key string) {
	if item, ok := c.items[key]; ok {
		c.bytes -= int64(len(item.result.Exchange))
		delete(c.items, key)
	}
}
