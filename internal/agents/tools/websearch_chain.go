package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// NamedSearchProvider is one ordered entry of a failover chain: a label used
// in error messages plus the provider it wraps.
type NamedSearchProvider struct {
	Name     string
	Provider SearchProvider
}

// providerStatusError reports a non-200 upstream status. It is a typed error
// so the chain provider can re-render it under the failing entry's name
// (design D2: "web.search: <Entry Name> returned status <code>").
type providerStatusError struct {
	name string
	code int
}

func (e *providerStatusError) Error() string {
	return fmt.Sprintf("web.search: %s returned status %d", e.name, e.code)
}

// NewChainSearchProvider composes ordered entries into a single SearchProvider
// (design D2). Attempts run in order; any attempt error (non-200 status,
// transport error, decode failure) advances to the next entry, while a
// success — even with zero results — returns immediately. When every entry
// fails, the last error is returned named after its entry. An empty entry
// list or an entry without a provider is a construction error.
func NewChainSearchProvider(entries []NamedSearchProvider) (SearchProvider, error) {
	if len(entries) == 0 {
		return nil, errors.New("web.search: no search providers configured")
	}
	for _, entry := range entries {
		if entry.Provider == nil {
			return nil, fmt.Errorf("web.search: search provider entry %q has no provider", entry.Name)
		}
	}
	chain := &chainProvider{entries: make([]NamedSearchProvider, len(entries))}
	copy(chain.entries, entries)
	return chain, nil
}

// chainProvider tries its entries positionally: strict priority, stateless
// failover on any error, first success wins.
type chainProvider struct {
	entries []NamedSearchProvider
}

// Search implements SearchProvider over the ordered entries.
func (c *chainProvider) Search(ctx context.Context, query string, num int) ([]SearchResult, error) {
	var lastErr error
	for _, entry := range c.entries {
		results, err := entry.Provider.Search(ctx, query, num)
		if err == nil {
			return results, nil
		}
		lastErr = namedSearchError(entry.Name, err)
	}
	return nil, lastErr
}

// namedSearchError re-renders a provider error under the failing entry's
// name: status errors become "web.search: <entry> returned status <code>",
// and every other error keeps its detail behind the entry name.
func namedSearchError(entry string, err error) error {
	var status *providerStatusError
	if errors.As(err, &status) {
		return fmt.Errorf("web.search: %s returned status %d", entry, status.code)
	}
	return &entrySearchError{entry: entry, err: err}
}

// entrySearchError carries an entry name in front of the underlying
// provider's error detail while preserving unwrap access to it.
type entrySearchError struct {
	entry string
	err   error
}

func (e *entrySearchError) Error() string {
	return fmt.Sprintf("web.search: %s: %s", e.entry, strings.TrimPrefix(e.err.Error(), "web.search: "))
}

func (e *entrySearchError) Unwrap() error { return e.err }
