package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/oniharnantyo/buddies/internal/agents/tools"
	"github.com/oniharnantyo/buddies/internal/store/fake"
)

// TestSettingsWebSearch_CatalogCoversRegistry proves the exposed provider
// catalog mirrors the runtime registry: same ids, labels, and credential kinds.
func TestSettingsWebSearch_CatalogCoversRegistry(t *testing.T) {
	svc := NewSettingsService(fake.New())

	providers, err := svc.WebSearchProviders(context.Background())
	require.NoError(t, err)
	registry := tools.SearchProviders()
	require.Len(t, providers, len(registry))
	for i, provider := range providers {
		require.Equal(t, registry[i].ID, provider.ID)
		require.Equal(t, registry[i].Label, provider.Label)
		require.Equal(t, string(registry[i].Credential), provider.Credential)
	}
}

// TestSettingsWebSearch_EmptyWhenUnset proves a fresh store yields the zero
// config with a non-nil, empty Entries slice — the pane renders an empty chain.
func TestSettingsWebSearch_EmptyWhenUnset(t *testing.T) {
	svc := NewSettingsService(fake.New())

	cfg, err := svc.WebSearchConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg.Entries)
	require.Empty(t, cfg.Entries)
	require.Equal(t, 0, cfg.RequestTimeoutSeconds)
}

// TestSettingsWebSearch_RoundTrip saves a two-provider chain and reads it
// back, proving order, credentials, assigned IDs, and the persisted JSON
// shape match the runtime's tools.WebSearchConfig exactly.
func TestSettingsWebSearch_RoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	err := svc.SaveWebSearchConfig(ctx, WebSearchConfig{
		Entries: []WebSearchChainEntry{
			{ID: "entry-1", Name: "Brave", Provider: tools.SearchProviderBrave, APIKey: "brave-key"},
			{Name: "", Provider: tools.SearchProviderSearXNG, BaseURL: "http://searx.local"},
		},
		RequestTimeoutSeconds: 15,
	})
	require.NoError(t, err)

	cfg, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)
	require.Len(t, cfg.Entries, 2)
	require.Equal(t, "entry-1", cfg.Entries[0].ID)
	require.Equal(t, tools.SearchProviderBrave, cfg.Entries[0].Provider)
	require.Equal(t, "brave-key", cfg.Entries[0].APIKey)
	require.NotEmpty(t, cfg.Entries[1].ID)
	require.Equal(t, tools.SearchProviderSearXNG, cfg.Entries[1].Provider)
	require.Equal(t, "http://searx.local", cfg.Entries[1].BaseURL)
	require.Equal(t, 15, cfg.RequestTimeoutSeconds)
}

// TestSettingsWebSearch_ReorderKeepsIDs reorders the chain and proves the
// entries keep their IDs and the new order persists.
func TestSettingsWebSearch_ReorderKeepsIDs(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	first := WebSearchConfig{Entries: []WebSearchChainEntry{
		{Provider: tools.SearchProviderBrave, APIKey: "k1"},
		{Provider: tools.SearchProviderExa, APIKey: "k2"},
	}}
	require.NoError(t, svc.SaveWebSearchConfig(ctx, first))
	saved, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)

	reordered := WebSearchConfig{Entries: []WebSearchChainEntry{
		saved.Entries[1], saved.Entries[0],
	}}
	require.NoError(t, svc.SaveWebSearchConfig(ctx, reordered))

	got, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)
	require.Len(t, got.Entries, 2)
	require.Equal(t, saved.Entries[1].ID, got.Entries[0].ID)
	require.Equal(t, saved.Entries[0].ID, got.Entries[1].ID)
	require.Equal(t, tools.SearchProviderExa, got.Entries[0].Provider)
	require.Equal(t, tools.SearchProviderBrave, got.Entries[1].Provider)
}

// TestSettingsWebSearch_RejectsMissingCredential proves the spec's rejection
// rule: an entry without its provider's required credential is an error
// naming the provider and the credential.
func TestSettingsWebSearch_RejectsMissingCredential(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	err := svc.SaveWebSearchConfig(ctx, WebSearchConfig{Entries: []WebSearchChainEntry{
		{Provider: tools.SearchProviderBrave},
	}})
	require.ErrorContains(t, err, "brave provider requires an API key")

	err = svc.SaveWebSearchConfig(ctx, WebSearchConfig{Entries: []WebSearchChainEntry{
		{Provider: tools.SearchProviderSearXNG},
	}})
	require.ErrorContains(t, err, "searxng provider requires a base URL")
}

// TestSettingsWebSearch_RejectsUnknownProvider proves entries outside the
// catalog are rejected.
func TestSettingsWebSearch_RejectsUnknownProvider(t *testing.T) {
	svc := NewSettingsService(fake.New())
	err := svc.SaveWebSearchConfig(context.Background(), WebSearchConfig{Entries: []WebSearchChainEntry{
		{Provider: "altavista", APIKey: "k"},
	}})
	require.ErrorContains(t, err, "unknown provider")
}

// TestSettingsWebSearch_JSONMatchesRuntimeShape proves the persisted document
// unmarshals into the runtime's tools.WebSearchConfig with the exact field
// names the web_search tool reads.
func TestSettingsWebSearch_JSONMatchesRuntimeShape(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfig{
		Entries: []WebSearchChainEntry{
			{Name: "Brave", Provider: tools.SearchProviderBrave, APIKey: "k1"},
		},
		RequestTimeoutSeconds: 20,
	}))

	raw, ok, err := fs.ToolSettings().Get(ctx, tools.NameWebSearch)
	require.NoError(t, err)
	require.True(t, ok)

	var cfg tools.WebSearchConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
	require.Len(t, cfg.Entries, 1)
	require.Equal(t, "Brave", cfg.Entries[0].Name)
	require.Equal(t, tools.SearchProviderBrave, cfg.Entries[0].Provider)
	require.Equal(t, "k1", cfg.Entries[0].APIKey)
	require.Equal(t, 20, cfg.RequestTimeoutSeconds)

	// The runtime accepts the document as a usable chain.
	_, err = tools.NewSearchProviderFromConfig(cfg, nil)
	require.NoError(t, err)
}
