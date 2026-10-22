package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// SearchConfigSource is the narrow seam the web_search tool uses to look up
// its persisted chain configuration (design D8). It is defined here so this
// package stays free of store imports; the tool_settings store adapter is
// wired by the composition root in a later task, and tests inject fakes.
type SearchConfigSource interface {
	// WebSearchConfig returns the persisted web_search chain config. A config
	// with no entries means the tool is not configured.
	WebSearchConfig(ctx context.Context) (WebSearchConfig, error)
}

// WebSearchConfig is the persisted web_search tool configuration, stored as
// JSON under tool key "web_search" in the tool_settings table (design D8).
// The JSON field names match what the Tools pane persists.
type WebSearchConfig struct {
	// Entries is the ordered provider chain; lower indexes try first.
	Entries []WebSearchChainEntry `json:"entries,omitempty"`
	// RequestTimeoutSeconds bounds each provider attempt; 0 means the default.
	RequestTimeoutSeconds int `json:"request_timeout_seconds,omitempty"`
}

// WebSearchChainEntry is one ordered entry of the configured failover chain.
// Exactly one of APIKey / BaseURL carries the credential, per the entry's
// provider's registry kind.
type WebSearchChainEntry struct {
	// ID is the stable identifier the settings layer assigns on first persist.
	ID string `json:"id,omitempty"`
	// Name labels the entry in transcripts and errors; falls back to Provider.
	Name string `json:"name"`
	// Provider is one of the SearchProvider* registry identifiers.
	Provider string `json:"provider"`
	// APIKey is the credential for api_key-kind providers.
	APIKey string `json:"api_key,omitempty"`
	// BaseURL is the credential for base_url-kind providers (SearXNG).
	BaseURL string `json:"base_url,omitempty"`
}

const (
	// webSearchDefaultTimeoutSeconds is the default per-attempt request
	// timeout applied to every provider entry in the chain.
	webSearchDefaultTimeoutSeconds = 10
	// webSearchMaxTimeoutSeconds bounds the per-attempt request timeout.
	webSearchMaxTimeoutSeconds = 60
	// webSearchWindow is the positional failover window: the first N entries
	// in list order serve; entries below it never do.
	webSearchWindow = 3
)

// errWebSearchNotConfigured is the invocation-time error for an agent with no
// usable search provider entries: explicit and actionable, raised through the
// lazy resolver before any network attempt, with no credential-free fallback.
// web_search always builds; the error surfaces as an error result on the tool
// call while the turn completes.
var errWebSearchNotConfigured = errors.New("web_search is not configured — add a provider in Settings → Tools")

// NewSearchProviderFromConfig builds the failover chain from the persisted
// config: each entry's provider is constructed with its kind's credential and
// the first webSearchWindow entries serve as the failover window. With no
// entries there is no fallback — errWebSearchNotConfigured is returned, which
// surfaces at first invocation as an error result. When client is nil each
// attempt is bounded by the config's request timeout (default 10s, max 60s);
// a non-nil client is used as-is (testing seam).
func NewSearchProviderFromConfig(cfg WebSearchConfig, client HTTPClient) (SearchProvider, error) {
	named := make([]NamedSearchProvider, 0, len(cfg.Entries))
	for _, entry := range cfg.Entries {
		info, known := SearchProviderInfoFor(entry.Provider)
		if !known {
			return nil, fmt.Errorf("web_search: unknown search provider %q", entry.Provider)
		}
		credential := entry.APIKey
		if info.Credential == SearchCredentialBaseURL {
			credential = entry.BaseURL
		}
		link, err := NewSearchProviderByCredential(entry.Provider, credential, searchAttemptClient(client, cfg))
		if err != nil {
			return nil, err
		}
		name := entry.Name
		if name == "" {
			name = entry.Provider
		}
		named = append(named, NamedSearchProvider{Name: name, Provider: link})
	}
	if len(named) == 0 {
		return nil, errWebSearchNotConfigured
	}
	if len(named) > webSearchWindow {
		named = named[:webSearchWindow]
	}
	return NewChainSearchProvider(named)
}

// searchAttemptClient resolves the per-attempt HTTP client: the injected one
// when set (testing), otherwise a fresh client bounded by the configured
// request timeout.
func searchAttemptClient(client HTTPClient, cfg WebSearchConfig) HTTPClient {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: webSearchAttemptTimeout(cfg.RequestTimeoutSeconds)}
}

// webSearchAttemptTimeout resolves the per-attempt request timeout: default
// 10s, clamped positive and at most 60s. Values that fail validation fall
// back to the default rather than disabling the bound.
func webSearchAttemptTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = webSearchDefaultTimeoutSeconds
	}
	if seconds > webSearchMaxTimeoutSeconds {
		seconds = webSearchMaxTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}
