package web

import (
	"context"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// FetchResult represents the result of a fetch operation.
type FetchResult struct {
	Content string `json:"content"`
}

// Fetcher defines the interface for swappable fetch providers.
type Fetcher interface {
	Fetch(ctx context.Context, url string, headers map[string]string) (FetchResult, error)
}

// FetcherFactory defines the signature for building a Fetcher.
type FetcherFactory func(cfg Config, resolver secrets.SecretResolver) (Fetcher, error)

var (
	fetchersMu sync.RWMutex
	fetchers   = make(map[string]FetcherFactory)
)

// RegisterFetcher registers a fetch provider factory.
func RegisterFetcher(name string, factory FetcherFactory) {
	fetchersMu.Lock()
	defer fetchersMu.Unlock()
	fetchers[name] = factory
}

// LookupFetcher retrieves a registered fetch provider factory.
func LookupFetcher(name string) (FetcherFactory, bool) {
	fetchersMu.RLock()
	defer fetchersMu.RUnlock()
	factory, ok := fetchers[name]
	return factory, ok
}
