package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/oniharnantyo/buddies/internal/agents/tools"
)

// The web_search chain methods on SettingsService: the Tools pane's provider
// catalog, chain editor read, and persist (design D8). The config persists as
// the runtime's own tools.WebSearchConfig JSON under tool key web_search in
// tool_settings, so the tool's lazy resolver consumes exactly what this
// service writes.

// WebSearchProviderInfo is one catalog search provider as the Tools pane
// renders it: the identifier stored in chain entries, its display label, and
// the credential kind the provider requires.
type WebSearchProviderInfo struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Credential string `json:"credential"`
}

// WebSearchChainEntry is one ordered entry of the web_search failover chain,
// as bound to the frontend. The JSON field names match the runtime's
// tools.WebSearchChainEntry — this is the same document, surfaced for editing.
type WebSearchChainEntry struct {
	// ID is the stable identifier assigned on first persist; edits and
	// reorders carry it unchanged.
	ID string `json:"id"`
	// Name labels the entry in transcripts and errors; empty falls back to
	// Provider at runtime.
	Name string `json:"name"`
	// Provider is one of the catalog provider identifiers.
	Provider string `json:"provider"`
	// APIKey is the credential for api_key-kind providers.
	APIKey string `json:"api_key,omitempty"`
	// BaseURL is the credential for base_url-kind providers (SearXNG).
	BaseURL string `json:"base_url,omitempty"`
}

// WebSearchConfig is the persisted web_search chain configuration, as bound
// to the frontend for the Tools pane's chain editor.
type WebSearchConfig struct {
	// Entries is the ordered provider chain; lower indexes try first.
	Entries []WebSearchChainEntry `json:"entries"`
	// RequestTimeoutSeconds bounds each provider attempt; 0 means the
	// runtime default.
	RequestTimeoutSeconds int `json:"request_timeout_seconds,omitempty"`
}

// WebSearchProviders returns the registered search providers with their
// credential requirements, ordered by ID. The result is never nil.
func (s *SettingsService) WebSearchProviders(_ context.Context) ([]WebSearchProviderInfo, error) {
	registry := tools.SearchProviders()
	out := make([]WebSearchProviderInfo, 0, len(registry))
	for _, info := range registry {
		out = append(out, WebSearchProviderInfo{ID: info.ID, Label: info.Label, Credential: string(info.Credential)})
	}
	return out, nil
}

// WebSearchConfig returns the persisted web_search chain configuration. A
// tool_settings row that never existed (or holds an empty document) comes
// back as the zero config with a non-nil, empty Entries slice.
func (s *SettingsService) WebSearchConfig(ctx context.Context) (WebSearchConfig, error) {
	raw, ok, err := s.store.ToolSettings().Get(ctx, tools.NameWebSearch)
	if err != nil {
		return WebSearchConfig{}, fmt.Errorf("get web_search config: %w", err)
	}
	cfg := WebSearchConfig{Entries: []WebSearchChainEntry{}}
	if !ok || raw == "" {
		return cfg, nil
	}
	var stored tools.WebSearchConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return WebSearchConfig{}, fmt.Errorf("decode web_search config: %w", err)
	}
	for _, entry := range stored.Entries {
		cfg.Entries = append(cfg.Entries, WebSearchChainEntry{
			ID:       entry.ID,
			Name:     entry.Name,
			Provider: entry.Provider,
			APIKey:   entry.APIKey,
			BaseURL:  entry.BaseURL,
		})
	}
	cfg.RequestTimeoutSeconds = stored.RequestTimeoutSeconds
	return cfg, nil
}

// SaveWebSearchConfig create-or-replaces the web_search chain configuration.
// Every entry's provider must be in the catalog, and an entry whose provider
// requires a credential must carry it — a chain entry without its API key or
// base URL is rejected with an error naming both. Entries are stored in the
// given order (the failover order); entries lacking an ID get one assigned.
func (s *SettingsService) SaveWebSearchConfig(ctx context.Context, cfg WebSearchConfig) error {
	stored := tools.WebSearchConfig{
		Entries:               make([]tools.WebSearchChainEntry, 0, len(cfg.Entries)),
		RequestTimeoutSeconds: cfg.RequestTimeoutSeconds,
	}
	if stored.RequestTimeoutSeconds < 0 {
		stored.RequestTimeoutSeconds = 0 // negative means the runtime default
	}
	used := make(map[string]bool, len(cfg.Entries))
	for _, entry := range cfg.Entries {
		info, known := tools.SearchProviderInfoFor(entry.Provider)
		if !known {
			return fmt.Errorf("save web_search config: unknown provider %q", entry.Provider)
		}
		id := entry.ID
		if id == "" {
			id = nextChainEntryID(used)
		}
		used[id] = true
		storedEntry := tools.WebSearchChainEntry{ID: id, Name: entry.Name, Provider: entry.Provider}
		switch info.Credential {
		case tools.SearchCredentialAPIKey:
			if entry.APIKey == "" {
				return fmt.Errorf("save web_search config: %s provider requires an API key", entry.Provider)
			}
			storedEntry.APIKey = entry.APIKey
		case tools.SearchCredentialBaseURL:
			if entry.BaseURL == "" {
				return fmt.Errorf("save web_search config: %s provider requires a base URL", entry.Provider)
			}
			storedEntry.BaseURL = entry.BaseURL
		default:
			return fmt.Errorf("save web_search config: provider %q has an unknown credential kind", entry.Provider)
		}
		stored.Entries = append(stored.Entries, storedEntry)
	}

	raw, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("encode web_search config: %w", err)
	}
	if err := s.store.ToolSettings().Set(ctx, tools.NameWebSearch, string(raw)); err != nil {
		return fmt.Errorf("save web_search config: %w", err)
	}
	return nil
}

// nextChainEntryID mints the first unused "entry-N" identifier.
func nextChainEntryID(used map[string]bool) string {
	for i := 1; ; i++ {
		id := fmt.Sprintf("entry-%d", i)
		if !used[id] {
			return id
		}
	}
}
