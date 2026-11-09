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
// as bound to the frontend. The id/name/provider/base_url JSON field names
// match the runtime's tools.WebSearchChainEntry — the persisted document —
// but this is the redacted read shape: a chain read never returns an entry's
// API key, only whether one is set and its non-secret hint.
type WebSearchChainEntry struct {
	// ID is the stable identifier assigned on first persist; edits and
	// reorders carry it unchanged, and SaveWebSearchConfig uses it to resolve
	// a blank submitted key to the stored credential.
	ID string `json:"id"`
	// Name labels the entry in transcripts and errors; empty falls back to
	// Provider at runtime.
	Name string `json:"name"`
	// Provider is one of the catalog provider identifiers.
	Provider string `json:"provider"`
	// BaseURL is the credential for base_url-kind providers (SearXNG). An
	// instance URL is not a secret, so it round-trips.
	BaseURL string `json:"base_url,omitempty"`
	// KeySet reports whether the stored entry carries an API key.
	KeySet bool
	// KeyHint is the key's last 4 runes; empty when no key is stored.
	KeyHint string
}

// WebSearchConfig is the read shape of the persisted web_search chain
// configuration, as bound to the frontend for the Tools pane's chain editor.
type WebSearchConfig struct {
	// Entries is the ordered provider chain; lower indexes try first.
	Entries []WebSearchChainEntry `json:"entries"`
	// RequestTimeoutSeconds bounds each provider attempt; 0 means the
	// runtime default.
	RequestTimeoutSeconds int `json:"request_timeout_seconds,omitempty"`
}

// WebSearchChainEntryInput is the write shape of one chain entry, as bound to
// the frontend. The JSON field names match the runtime document, so the
// pane's payload is unchanged. The API key is write-only: a blank key keeps
// the stored credential for an entry carrying its stable ID, a non-blank key
// replaces it — the key is never handed back out on a read.
type WebSearchChainEntryInput struct {
	// ID is the stable identifier of an existing entry; empty on a new
	// entry, which must carry its credential.
	ID string `json:"id"`
	// Name labels the entry in transcripts and errors; empty falls back to
	// Provider at runtime.
	Name string `json:"name"`
	// Provider is one of the catalog provider identifiers.
	Provider string `json:"provider"`
	// APIKey is the credential for api_key-kind providers; blank keeps the
	// stored key.
	APIKey string `json:"api_key,omitempty"`
	// BaseURL is the credential for base_url-kind providers (SearXNG).
	BaseURL string `json:"base_url,omitempty"`
}

// WebSearchConfigInput is the write shape of the web_search chain
// configuration, as bound to the frontend for the Tools pane's chain editor.
type WebSearchConfigInput struct {
	// Entries is the ordered provider chain; lower indexes try first.
	Entries []WebSearchChainEntryInput `json:"entries"`
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
// chain read never returns an entry's API key: each entry is projected onto
// the redacted read shape — whether a key is set and its last-4 hint. A
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
			BaseURL:  entry.BaseURL,
			KeySet:   entry.APIKey != "",
			KeyHint:  keyHint(entry.APIKey),
		})
	}
	cfg.RequestTimeoutSeconds = stored.RequestTimeoutSeconds
	return cfg, nil
}

// SaveWebSearchConfig create-or-replaces the web_search chain configuration,
// merging incoming entries against the stored config by stable entry ID
// (design D4): a blank API key on an entry whose ID exists in storage keeps
// the stored key, a non-blank key replaces it, and a blank key that cannot
// resolve — a new entry, or an ID absent from storage — is rejected with the
// provider named. After the merge, every entry's provider must be in the
// catalog and carry its provider's required credential, so a provider change
// with a blank key is validated against the new provider's requirement.
// Entries are stored in the given order (the failover order); entries
// lacking an ID get one assigned. The merged API keys persist but never
// return on a read.
func (s *SettingsService) SaveWebSearchConfig(ctx context.Context, cfg WebSearchConfigInput) error {
	// The stored config loads first: blank submitted keys resolve against it
	// by entry ID.
	storedRaw, _, err := s.store.ToolSettings().Get(ctx, tools.NameWebSearch)
	if err != nil {
		return fmt.Errorf("save web_search config: %w", err)
	}
	var stored tools.WebSearchConfig
	if storedRaw != "" {
		if err := json.Unmarshal([]byte(storedRaw), &stored); err != nil {
			return fmt.Errorf("save web_search config: decode stored config: %w", err)
		}
	}
	byID := make(map[string]tools.WebSearchChainEntry, len(stored.Entries))
	for _, entry := range stored.Entries {
		byID[entry.ID] = entry
	}

	merged := tools.WebSearchConfig{
		Entries:               make([]tools.WebSearchChainEntry, 0, len(cfg.Entries)),
		RequestTimeoutSeconds: cfg.RequestTimeoutSeconds,
	}
	if merged.RequestTimeoutSeconds < 0 {
		merged.RequestTimeoutSeconds = 0 // negative means the runtime default
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
		mergedEntry := tools.WebSearchChainEntry{ID: id, Name: entry.Name, Provider: entry.Provider}
		switch info.Credential {
		case tools.SearchCredentialAPIKey:
			apiKey := entry.APIKey
			if apiKey == "" && entry.ID != "" {
				// Blank means keep the stored key, addressed by the entry's
				// stable ID; a minted ID marks a new entry, which must carry
				// its key.
				if prev, found := byID[entry.ID]; found {
					apiKey = prev.APIKey
				}
			}
			if apiKey == "" {
				return fmt.Errorf("save web_search config: %s provider requires an API key", entry.Provider)
			}
			mergedEntry.APIKey = apiKey
		case tools.SearchCredentialBaseURL:
			// A base URL is not a secret and always round-trips; blank is
			// simply a missing credential.
			if entry.BaseURL == "" {
				return fmt.Errorf("save web_search config: %s provider requires a base URL", entry.Provider)
			}
			mergedEntry.BaseURL = entry.BaseURL
		default:
			return fmt.Errorf("save web_search config: provider %q has an unknown credential kind", entry.Provider)
		}
		merged.Entries = append(merged.Entries, mergedEntry)
	}

	raw, err := json.Marshal(merged)
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
