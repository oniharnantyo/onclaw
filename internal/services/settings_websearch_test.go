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
// back, proving order, assigned IDs, key hints, the unredacted base URL, and
// the persisted JSON shape match the runtime's tools.WebSearchConfig exactly.
func TestSettingsWebSearch_RoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	err := svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{ID: "entry-1", Name: "Brave", Provider: tools.SearchProviderBrave, APIKey: "brave-secret-1234"},
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
	require.True(t, cfg.Entries[0].KeySet)
	require.Equal(t, "1234", cfg.Entries[0].KeyHint)
	require.NotEmpty(t, cfg.Entries[1].ID)
	require.Equal(t, tools.SearchProviderSearXNG, cfg.Entries[1].Provider)
	// A base URL is not a secret; it round-trips unchanged.
	require.Equal(t, "http://searx.local", cfg.Entries[1].BaseURL)
	require.False(t, cfg.Entries[1].KeySet)
	require.Empty(t, cfg.Entries[1].KeyHint)
	require.Equal(t, 15, cfg.RequestTimeoutSeconds)
}

// TestSettingsWebSearch_ReadNeverReturnsKey proves the redaction invariant on
// the read path: the marshaled chain read carries no api_key field at all —
// only the KeySet state and the hint — whatever is stored.
func TestSettingsWebSearch_ReadNeverReturnsKey(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{ID: "entry-1", Name: "Brave", Provider: tools.SearchProviderBrave, APIKey: "brave-secret-1234"},
		},
	}))

	cfg, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)
	require.True(t, cfg.Entries[0].KeySet)
	require.Equal(t, "1234", cfg.Entries[0].KeyHint)

	blob, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(blob), "brave-secret-1234")
	require.NotContains(t, string(blob), "api_key")
}

// TestSettingsWebSearch_ReorderAndRenameKeepStoredKeys reorders and renames
// the chain with blank key fields — what the redacted pane sends on an
// untouched edit — proving the merge resolves every carried ID to its stored
// key and the runtime still authenticates the saved chain.
func TestSettingsWebSearch_ReorderAndRenameKeepStoredKeys(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{Provider: tools.SearchProviderBrave, APIKey: "brave-key"},
			{Provider: tools.SearchProviderExa, APIKey: "exa-key"},
		},
	}))
	saved, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)

	// Swap the entries and rename one; key fields stay blank.
	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{ID: saved.Entries[1].ID, Provider: saved.Entries[1].Provider},
			{ID: saved.Entries[0].ID, Name: "Brave Search", Provider: saved.Entries[0].Provider},
		},
	}))

	got, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)
	require.Len(t, got.Entries, 2)
	require.Equal(t, saved.Entries[1].ID, got.Entries[0].ID)
	require.Equal(t, tools.SearchProviderExa, got.Entries[0].Provider)
	require.Equal(t, saved.Entries[0].ID, got.Entries[1].ID)
	require.Equal(t, "Brave Search", got.Entries[1].Name)
	require.Equal(t, tools.SearchProviderBrave, got.Entries[1].Provider)
	// Hints prove the keys survived untouched.
	require.Equal(t, keyHint("exa-key"), got.Entries[0].KeyHint)
	require.Equal(t, keyHint("brave-key"), got.Entries[1].KeyHint)

	// The persisted document still carries the stored keys, and the runtime
	// accepts it as a usable chain.
	raw, ok, err := fs.ToolSettings().Get(ctx, tools.NameWebSearch)
	require.NoError(t, err)
	require.True(t, ok)
	var persisted tools.WebSearchConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &persisted))
	require.Len(t, persisted.Entries, 2)
	require.Equal(t, "exa-key", persisted.Entries[0].APIKey)
	require.Equal(t, "brave-key", persisted.Entries[1].APIKey)
	_, err = tools.NewSearchProviderFromConfig(persisted, nil)
	require.NoError(t, err)
}

// TestSettingsWebSearch_RejectsMissingCredential proves the spec's rejection
// rule: an entry without its provider's required credential is an error
// naming the provider and the credential — a new entry, with no stored key
// to resolve, most of all.
func TestSettingsWebSearch_RejectsMissingCredential(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	err := svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{Entries: []WebSearchChainEntryInput{
		{Provider: tools.SearchProviderBrave},
	}})
	require.ErrorContains(t, err, "brave provider requires an API key")

	err = svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{Entries: []WebSearchChainEntryInput{
		{Provider: tools.SearchProviderSearXNG},
	}})
	require.ErrorContains(t, err, "searxng provider requires a base URL")
}

// TestSettingsWebSearch_RejectsUnresolvableBlankKey proves a blank key on an
// ID the store does not know cannot resolve: the save is rejected with the
// provider named, not silently persisted key-less.
func TestSettingsWebSearch_RejectsUnresolvableBlankKey(t *testing.T) {
	ctx := context.Background()
	svc := NewSettingsService(fake.New())

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{Provider: tools.SearchProviderBrave, APIKey: "brave-key"},
		},
	}))

	err := svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{Entries: []WebSearchChainEntryInput{
		{ID: "entry-9", Provider: tools.SearchProviderExa},
	}})
	require.ErrorContains(t, err, "exa provider requires an API key")
}

// TestSettingsWebSearch_RejectsUnknownProvider proves entries outside the
// catalog are rejected.
func TestSettingsWebSearch_RejectsUnknownProvider(t *testing.T) {
	svc := NewSettingsService(fake.New())
	err := svc.SaveWebSearchConfig(context.Background(), WebSearchConfigInput{Entries: []WebSearchChainEntryInput{
		{Provider: "altavista", APIKey: "k"},
	}})
	require.ErrorContains(t, err, "unknown provider")
}

// TestSettingsWebSearch_ProviderChangeKeepsStoredKey proves the merge runs
// before validation (design D4): an entry that swaps providers while leaving
// its key blank keeps the stored credential, and the merged entry is
// validated against the new provider's requirement.
func TestSettingsWebSearch_ProviderChangeKeepsStoredKey(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{Provider: tools.SearchProviderBrave, APIKey: "brave-key"},
		},
	}))
	saved, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)

	// The same ID, now exa with a blank key field: valid — the stored key
	// resolves and exa requires an API key.
	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{ID: saved.Entries[0].ID, Provider: tools.SearchProviderExa},
		},
	}))
	raw, ok, err := fs.ToolSettings().Get(ctx, tools.NameWebSearch)
	require.NoError(t, err)
	require.True(t, ok)
	var persisted tools.WebSearchConfig
	require.NoError(t, json.Unmarshal([]byte(raw), &persisted))
	require.Len(t, persisted.Entries, 1)
	require.Equal(t, tools.SearchProviderExa, persisted.Entries[0].Provider)
	require.Equal(t, "brave-key", persisted.Entries[0].APIKey)

	// The same ID changed to searxng with the key field still blank fails the
	// new provider's requirement: a base URL, which the merge cannot supply.
	err = svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{ID: saved.Entries[0].ID, Provider: tools.SearchProviderSearXNG},
		},
	})
	require.ErrorContains(t, err, "searxng provider requires a base URL")
}

// TestSettingsWebSearch_NonBlankKeyReplaces proves a typed key wins over the
// merge: the stored credential is overwritten, not kept.
func TestSettingsWebSearch_NonBlankKeyReplaces(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{Provider: tools.SearchProviderBrave, APIKey: "old-key"},
		},
	}))
	saved, err := svc.WebSearchConfig(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
			{ID: saved.Entries[0].ID, Provider: tools.SearchProviderBrave, APIKey: "new-key"},
		},
	}))

	raw, ok, err := fs.ToolSettings().Get(ctx, tools.NameWebSearch)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, raw, "new-key")
	require.NotContains(t, raw, "old-key")
}

// TestSettingsWebSearch_JSONMatchesRuntimeShape proves the persisted document
// unmarshals into the runtime's tools.WebSearchConfig with the exact field
// names the web_search tool reads.
func TestSettingsWebSearch_JSONMatchesRuntimeShape(t *testing.T) {
	ctx := context.Background()
	fs := fake.New()
	svc := NewSettingsService(fs)

	require.NoError(t, svc.SaveWebSearchConfig(ctx, WebSearchConfigInput{
		Entries: []WebSearchChainEntryInput{
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
