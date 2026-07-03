package web

import (
	"context"
	"sync"

	"github.com/oniharnantyo/onclaw/internal/secrets"
)

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Searcher defines the interface for swappable search providers.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// SearcherFactory defines the signature for building a Searcher.
type SearcherFactory func(cfg Config, resolver secrets.SecretResolver) (Searcher, error)

var (
	searchersMu sync.RWMutex
	searchers   = make(map[string]SearcherFactory)
)

// RegisterSearcher registers a search provider factory.
func RegisterSearcher(name string, factory SearcherFactory) {
	searchersMu.Lock()
	defer searchersMu.Unlock()
	searchers[name] = factory
}

// LookupSearcher retrieves a registered search provider factory.
func LookupSearcher(name string) (SearcherFactory, bool) {
	searchersMu.RLock()
	defer searchersMu.RUnlock()
	factory, ok := searchers[name]
	return factory, ok
}
