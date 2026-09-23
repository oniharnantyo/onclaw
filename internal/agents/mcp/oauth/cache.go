package oauth

import (
	"sync"
	"time"
)

// discoveryCache is the in-memory TTL cache over resolved Metadata, keyed by
// normalized server URL (add-mcp-oauth-client design.md D2). Only successful
// discoveries are cached: a failure re-runs the chain on the next call, so a
// provider fixing its metadata is picked up immediately.
//
// Concurrency-safe. Entries are shared pointers published once and never
// mutated afterward — callers treat Metadata as read-only.
type discoveryCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cacheEntry
}

type cacheEntry struct {
	meta      *Metadata
	expiresAt time.Time
}

func newDiscoveryCache(ttl time.Duration) *discoveryCache {
	return &discoveryCache{ttl: ttl, entries: make(map[string]cacheEntry)}
}

// get returns the entry for key, deleting and reporting nothing when expired.
func (c *discoveryCache) get(key string) *Metadata {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	if now.After(entry.expiresAt) {
		delete(c.entries, key)
		return nil
	}
	return entry.meta
}

func (c *discoveryCache) put(key string, meta *Metadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{meta: meta, expiresAt: time.Now().Add(c.ttl)}
}

func (c *discoveryCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
